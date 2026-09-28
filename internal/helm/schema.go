package helm

import (
	"context"
	"fmt"
)

// The values schema of the chart that is actually deployed (etappe 107).
//
// The settings form rendered and validated against a schema compiled into the binary,
// and only one was ever shipped: 26.5.x. Every other version fell through to it without
// a word — the loader picks the matching major.minor or, failing that, whatever file it
// saw last. The installation the form was used on runs 26.8.0, so fields added since were
// missing, removed ones were offered, and "valid" meant valid for a chart that was not
// running.
//
// Helm already keeps the right answer: a release stores its complete chart, including
// values.schema.json, in the release secret. Reading it from there needs no network
// (edge case 5), cannot drift from what is deployed, and follows an upgrade without a
// restart — the ESS version used to be read once at startup and then trusted forever.

// DeployedSchema is the schema of the chart in a release's newest revision, and the
// chart version it came from.
//
// The newest revision, not the newest *successful* one: after a failed upgrade the
// failed revision's chart is what is on the cluster and what the next attempt starts
// from. An empty schema is returned as an error so the caller falls back visibly rather
// than rendering a form with no fields.
func (c *Client) DeployedSchema(name string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	id, err := c.probeNewestRelease(ctx, name)
	if err != nil {
		return nil, "", fmt.Errorf("release %s: %w", name, err)
	}

	c.relMu.Lock()
	if s, ok := c.schemaCache[name]; ok && s.identity == id {
		c.relMu.Unlock()
		return s.schema, s.version, nil
	}
	c.relMu.Unlock()

	// One revision, decoded by Helm's own storage layer — the same ~500 ms read the
	// release view uses, and only when the release secret has changed.
	rel, id, err := c.newestRelease(name)
	if err != nil {
		return nil, "", err
	}
	if rel.Chart == nil || len(rel.Chart.Schema) == 0 {
		return nil, "", fmt.Errorf("the deployed chart of %s carries no values schema", name)
	}
	version := ""
	if rel.Chart.Metadata != nil {
		version = rel.Chart.Metadata.Version
	}

	c.relMu.Lock()
	if c.schemaCache == nil {
		c.schemaCache = map[string]memoisedSchema{}
	}
	c.schemaCache[name] = memoisedSchema{identity: id, schema: rel.Chart.Schema, version: version}
	c.relMu.Unlock()

	return rel.Chart.Schema, version, nil
}

type memoisedSchema struct {
	identity releaseIdentity
	schema   []byte
	version  string
}
