package handlers

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Reconciling MatrixCtrl's own MAS client registration.
//
// The registration used to be all-or-nothing: ConnectOIDC wrote the fragment on a
// fresh instance and refused with 409 for ever after. So an instance registered
// before a field was added to the generator could never acquire it — the operator
// had to hand-edit YAML, which is the activity this product exists to remove.
//
// It showed up as MAS asking "Continue to 01KSPV9ZMR7NB4B2BBWMPYSD1P?" on the
// consent screen: a ULID where the application name belongs, at the exact moment
// someone is deciding whether to trust this thing with their homeserver.

// masClientRepair describes what a reconcile would do or did.
type masClientRepair struct {
	// Changed lists the fields added, in the order they were added. Empty means the
	// stored fragment already matched.
	Changed []string
	// Config is the fragment to store. Equal to the input when nothing changed.
	Config string
}

// repairMASClientConfig adds fields the current generator writes and the stored
// fragment lacks.
//
// Deliberately narrow. It never regenerates the client ID or secret — re-running
// "connect" on a working instance must not invalidate the credential that instance
// is authenticating with — and it never overwrites a value that is already there,
// because an operator who changed a redirect URI on purpose should keep it.
func repairMASClientConfig(existing, clientName string) (*masClientRepair, error) {
	if clientName == "" {
		return nil, fmt.Errorf("no client name to set")
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(existing), &doc); err != nil {
		// Refused rather than replaced. A fragment this cannot read is one somebody
		// may have edited by hand, and overwriting it would destroy that edit while
		// claiming to have repaired something.
		return nil, fmt.Errorf("the stored MAS client config is not valid YAML: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("the stored MAS client config is not a mapping")
	}

	clients := mappingValue(doc.Content[0], "clients")
	if clients == nil || clients.Kind != yaml.SequenceNode || len(clients.Content) == 0 {
		return nil, fmt.Errorf("the stored MAS client config has no clients")
	}

	out := &masClientRepair{Config: existing}
	for _, client := range clients.Content {
		if client.Kind != yaml.MappingNode {
			continue
		}
		if mappingValue(client, "client_name") != nil {
			continue
		}
		// Inserted right after client_id so the generated block keeps reading the
		// way the generator writes it, rather than growing an appendix.
		insertAfter(client, "client_id", "client_name", clientName)
		out.Changed = append(out.Changed, "client_name")
	}

	if len(out.Changed) == 0 {
		return out, nil
	}

	repaired, err := yaml.Marshal(doc.Content[0])
	if err != nil {
		return nil, fmt.Errorf("could not re-encode the MAS client config: %w", err)
	}
	out.Config = string(repaired)
	return out, nil
}

// mappingValue returns the value node for a key, or nil.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// insertAfter places key/value directly after an existing key, appending when that
// key is absent. Mapping content is a flat key,value,key,value list, so both
// elements move together or the document is corrupted.
func insertAfter(m *yaml.Node, afterKey, key, value string) {
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	// Quoted, because a client name is display text: left plain, a name like `Yes`
	// or `2026-01-01` would be parsed back as a boolean or a timestamp.
	v := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: yaml.DoubleQuotedStyle}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == afterKey {
			at := i + 2
			rest := append([]*yaml.Node{}, m.Content[at:]...)
			m.Content = append(append(m.Content[:at], k, v), rest...)
			return
		}
	}
	m.Content = append(m.Content, k, v)
}

// missingMASClientFields reports which fields the registered client config lacks,
// by running the repair as a dry run — the check and the fix are then the same
// code, and cannot disagree about what "incomplete" means.
//
// Returns nothing when there is no client, when the fragment cannot be read, or
// when it is already complete. All three are "nothing to offer the operator": an
// unreadable fragment is not something to propose an automatic repair for.
func missingMASClientFields(merged map[string]interface{}) []string {
	existing, _ := nestedGet(merged, "matrixAuthenticationService", "additional", "0-matrixctrl-client", "config").(string)
	if existing == "" {
		return nil
	}
	repair, err := repairMASClientConfig(existing, masClientDisplayName)
	if err != nil {
		return nil
	}
	return repair.Changed
}
