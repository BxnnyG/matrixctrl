package helm

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
	"time"

	cfgschema "github.com/bxnnyg/matrixctrl/internal/config/schema"
)

// The schema the settings form uses comes from the chart that is running.
//
// Until etappe 107 it came from the binary, and only 26.5.x was ever embedded — every
// other version fell back to it silently. This asks the live release and checks that
// the answer names the chart version the release actually runs, and that it is a real
// schema rather than an empty one.
func TestLiveDeployedSchema(t *testing.T) {
	if os.Getenv("RUN_LIVE") == "" {
		t.Skip("set RUN_LIVE=1")
	}
	c, err := New("ess")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	schema, version, err := c.DeployedSchema("ess")
	if err != nil {
		t.Fatalf("deployed schema: %v", err)
	}
	cold := time.Since(start)

	rel, err := c.GetRelease("ess")
	if err != nil {
		t.Fatal(err)
	}
	if version != rel.Version {
		t.Errorf("the schema must be the running chart's: schema says %q, release runs %q", version, rel.Version)
	}

	var parsed struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("not a JSON schema: %v", err)
	}
	for _, want := range []string{"synapse", "matrixAuthenticationService", "postgres"} {
		if _, ok := parsed.Properties[want]; !ok {
			t.Errorf("schema has no %q section", want)
		}
	}

	// What the embedded fallback would have hidden or invented — logged, because it is
	// the concrete size of the error this replaces.
	if emb, err := cfgschema.Get(version); err == nil {
		var old struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(emb, &old) == nil {
			var added, gone []string
			for k := range parsed.Properties {
				if _, ok := old.Properties[k]; !ok {
					added = append(added, k)
				}
			}
			for k := range old.Properties {
				if _, ok := parsed.Properties[k]; !ok {
					gone = append(gone, k)
				}
			}
			sort.Strings(added)
			sort.Strings(gone)
			t.Logf("gegenüber dem eingebauten %s: neu %v · entfallen %v", cfgschema.VersionOf(version), added, gone)
		}
	}

	// Second read: the release secret has not changed, so this must come from the cache.
	start = time.Now()
	if _, _, err := c.DeployedSchema("ess"); err != nil {
		t.Fatal(err)
	}
	warm := time.Since(start)
	t.Logf("Chart %s · %d Abschnitte · %.0f KB · kalt %s, warm %s",
		version, len(parsed.Properties), float64(len(schema))/1024, cold.Round(time.Millisecond), warm.Round(time.Millisecond))
	if warm > cold {
		t.Logf("warm read was not faster than cold — the cache is not being hit")
	}
}
