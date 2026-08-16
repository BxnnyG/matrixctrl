package helm

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The history page asked a question that costs 3.2–4.6 s and mostly wanted an
// answer the labels already hold (etappe 39, P2-22).
//
// `action.NewHistory` fetches and decodes every revision of the release — 14 on the
// production instance — to fill a table of four columns. Two of those columns are
// in the secret's labels and free; the other two are immutable once written, so
// they are worth decoding exactly once.
//
// This is E20's move applied one page over, but *not* by copying it: two things
// that made E20 work do not transfer, and both were measured rather than assumed.
// See bulkDecodeThreshold and the modifiedAt note in listRevisionMeta.

const (
	// bulkDecodeThreshold decides between one History call and N targeted Gets.
	//
	// Measured on 14 revisions of the production release:
	//
	//	14 × Releases.Get     →  7.328 s
	//	 1 × Releases.History →  5.270 s
	//
	// Per-revision fetching is *slower* in bulk — each is its own round trip — so
	// the obvious "decode only what you need" refinement makes a cold read 40 %
	// worse. It only wins for the incremental case, which is the normal one: after
	// an upgrade exactly one revision is new. Break-even sits near ten.
	bulkDecodeThreshold = 10

	// historyProbeTimeout bounds the **label list** only — the 56 ms part.
	//
	// It deliberately does not cover the decodes that may follow: those go through
	// Helm's storage driver, which carries its own context, and a bulk fill legitimately
	// takes ~5 s. Wrapping that in a 5 s deadline would abort the very work the fast
	// path exists to do once. Exceeding this costs the old latency, never a wrong
	// answer.
	historyProbeTimeout = probeTimeout

	// persistTimeout bounds the write-behind. Short: nothing waits on it, and a
	// database that is slow to accept a cache row should not hold a request open.
	persistTimeout = 5 * time.Second
)

// revisionMeta is what a release secret's labels reveal without decoding it.
type revisionMeta struct {
	Revision int
	// Status is read fresh on every call and deliberately never cached: it is the
	// one field of a revision that changes after the revision is written
	// (deployed → superseded, pending-upgrade → deployed).
	Status string
}

// revisionFacts are the fields that require a decode and then never change again.
//
// A revision's chart and its deployment time are fixed at the moment Helm writes
// the secret. Nothing rewrites them — a rollback creates a *new* highest revision
// rather than editing an old one — so caching them for the life of the process is
// not a staleness trade, it is arithmetic.
type revisionFacts struct {
	Chart string
	// DeployedAt is stored whole rather than as Unix seconds. The first version
	// truncated to seconds, which produced rows that differed from the fallback's
	// by up to a second — the live cross-check caught it. There was never a reason
	// to round: nothing here is doing arithmetic on it.
	DeployedAt time.Time
}

// listRevisionMeta returns every revision of a release with its current status,
// read from labels only.
//
// It does **not** return a timestamp, although the secrets carry a `modifiedAt`
// label and E20 reads it. That label is not per-revision: on the production
// release nine revisions spanning ten weeks share the value 1769459689, while
// their real `LastDeployed` values are all different. E20 uses it correctly, as a
// cache-invalidation key for the newest revision, and using it as a displayed
// timestamp would have put confidently wrong dates in front of the operator.
func (c *Client) listRevisionMeta(ctx context.Context, name string) ([]revisionMeta, error) {
	if c.meta == nil {
		return nil, fmt.Errorf("no metadata client")
	}
	list, err := c.meta.Resource(secretGVR).Namespace(c.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "owner=" + helmSecretOwner + ",name=" + name,
	})
	if err != nil {
		return nil, fmt.Errorf("list release secrets: %w", err)
	}

	out := make([]revisionMeta, 0, len(list.Items))
	for _, item := range list.Items {
		rev, ok := revisionOf(item.Name, item.Labels["version"])
		if !ok {
			continue
		}
		out = append(out, revisionMeta{Revision: rev, Status: item.Labels["status"]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("release: not found")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out, nil
}

// listHistoryFast is the cheap path. It returns an error rather than a partial
// answer whenever anything is unavailable, so the caller can fall back whole.
func (c *Client) listHistoryFast(name string, max int) ([]RevisionEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), historyProbeTimeout)
	defer cancel()

	metas, err := c.listRevisionMeta(ctx, name)
	if err != nil {
		return nil, err
	}

	// Prune against the **full** list, before truncation.
	//
	// Getting this order wrong is not theoretical: pruning against the truncated
	// list makes `max` destructive. A call with max=5 would evict the facts for
	// every older revision, so the next call with max=10 pays a fresh decode for
	// revisions it had already decoded. Measured at 2.958 s where it should have
	// been 40 ms, which is how the ordering bug announced itself.
	live := make(map[int]bool, len(metas))
	for _, m := range metas {
		live[m.Revision] = true
	}
	c.pruneRevisionFacts(name, live)

	// Truncate before decoding anything, which is what makes `max` mean something.
	// Helm's own History action accepts a Max and never reads it, so asking for ten
	// used to return fourteen and cost the same as asking for thirty.
	if max > 0 && len(metas) > max {
		metas = metas[:max]
	}

	missing := c.missingRevisions(name, metas)

	// Ask the store before decoding anything. This is what makes the cold read a
	// once-per-*revision* cost instead of a once-per-*process* one: the map dies
	// with the pod, and the operator measured 7.7 s because a deploy had reset it
	// (etappe 42).
	if len(missing) > 0 && c.facts != nil {
		if stored, err := c.facts.LoadRevisionFacts(ctx, name); err == nil {
			for rev, f := range stored {
				c.storeRevisionFacts(name, rev, revisionFacts{Chart: f.Chart, DeployedAt: f.DeployedAt})
			}
			missing = c.missingRevisions(name, metas)
		}
		// A store that cannot be read costs the decode it was meant to avoid, and
		// nothing else. It is not reported: the page is correct either way, and a
		// warning about a cache is noise on a screen about upgrades.
	}

	if len(missing) > 0 {
		if err := c.fillRevisionFacts(name, missing); err != nil {
			return nil, err
		}
		// A fresh context, deliberately. `ctx` carries historyProbeTimeout, which
		// bounds the 56 ms label list — by the time a bulk decode has finished it is
		// long expired, so persisting under it would fail every single time and the
		// store would silently never fill.
		c.persistRevisions(name, missing)
	}

	entries := make([]RevisionEntry, 0, len(metas))
	for _, m := range metas {
		facts, ok := c.revisionFacts(name, m.Revision)
		if !ok {
			// A revision that survived the fill without producing facts means the
			// fill and the label list disagree about what exists. Returning a row
			// with a blank chart would look like data; failing hands the caller to
			// the slow path, which is authoritative.
			return nil, fmt.Errorf("revision %d missing after fill", m.Revision)
		}
		entries = append(entries, RevisionEntry{
			Revision:   m.Revision,
			Status:     m.Status,
			Chart:      facts.Chart,
			DeployedAt: facts.DeployedAt,
		})
	}
	return entries, nil
}

// fillRevisionFacts decodes what is missing, choosing the cheaper shape.
//
// See bulkDecodeThreshold: N targeted Gets beat one History only while N is small,
// and the crossover was measured, not reasoned about.
func (c *Client) fillRevisionFacts(name string, missing []int) error {
	if len(missing) <= bulkDecodeThreshold {
		for _, rev := range missing {
			rel, err := c.cfg.Releases.Get(name, rev)
			if err != nil {
				return fmt.Errorf("get revision %d: %w", rev, err)
			}
			c.storeRevisionFacts(name, rev, factsOf(rel))
		}
		return nil
	}

	rels, err := c.cfg.Releases.History(name)
	if err != nil {
		return fmt.Errorf("history %s: %w", name, err)
	}
	for _, rel := range rels {
		c.storeRevisionFacts(name, rel.Version, factsOf(rel))
	}
	return nil
}

// missingRevisions is the set of revisions whose facts are not in memory.
func (c *Client) missingRevisions(name string, metas []revisionMeta) []int {
	out := make([]int, 0, len(metas))
	for _, m := range metas {
		if _, ok := c.revisionFacts(name, m.Revision); !ok {
			out = append(out, m.Revision)
		}
	}
	return out
}

// persistRevisions writes freshly decoded facts to the store, best effort.
//
// Failing to persist costs the next process the same decode. That is the situation
// this feature exists to improve, not a correctness problem, so it does not
// propagate: an upgrade page that refuses to render because a cache write failed
// would be a worse product than a slow one.
func (c *Client) persistRevisions(name string, revs []int) {
	if c.facts == nil || len(revs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	out := make(map[int]RevisionFact, len(revs))
	for _, rev := range revs {
		if f, ok := c.revisionFacts(name, rev); ok {
			out[rev] = RevisionFact{Chart: f.Chart, DeployedAt: f.DeployedAt}
		}
	}
	if len(out) == 0 {
		return
	}
	if err := c.facts.SaveRevisionFacts(ctx, name, out); err != nil {
		log.Printf("helm: could not persist revision facts for %s: %v", name, err)
	}
}
