// Package tasks is the settings page for people who do not know the chart (etappe 113).
//
// The full form lists what the Helm chart has — 1 400 settings, alphabetical, in the
// chart's words; Synapse opens with `Additional`, `Appservices`, `Extra Args`. What a
// person running a homeserver looks for is a handful of things with German names:
// who may register, where the services live, how much memory Synapse gets. Those are
// defined here, once, as data: the API serves them and the page renders what it gets.
//
// A field has one of two sources:
//
//   - a Helm value (`postgres.resources.requests.memory`), read from the merged
//     settings and written comment-preserving into the section file that owns it;
//   - a key of a service's own configuration (MAS `account.password_registration_enabled`),
//     which the chart only accepts as text under `<component>.additional.<name>.config`.
//     MatrixCtrl owns exactly one such block per component, `matrixctrl-tasks`, and
//     writes nowhere else. A key another block already sets is shown, not editable —
//     two places for one value, one of them silently winning, is the confusion this
//     package exists to remove.
package tasks

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Block is the additional-config key MatrixCtrl owns in each component.
const Block = "matrixctrl-tasks"

// Kind is how a field is edited.
type Kind string

const (
	Bool     Kind = "bool"
	Text     Kind = "text"
	Quantity Kind = "quantity" // Kubernetes quantity: 1Gi, 500m
)

// Source says where a value lives.
type Source struct {
	// Values is a dotted Helm value path. Exclusive with Component/Key.
	Values string
	// Component is the chart section whose additional config holds Key
	// ("matrixAuthenticationService", "synapse").
	Component string
	Key       string // dotted key inside that service's configuration
}

// Field is one setting as a person sees it.
type Field struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Help     string      `json:"help"`
	Kind     Kind        `json:"kind"`
	Default  interface{} `json:"default,omitempty"`
	Restarts string      `json:"restarts,omitempty"` // what applying it restarts, in words
	Warn     string      `json:"warn,omitempty"`     // shown when the value is changed
	Locked   string      `json:"locked,omitempty"`   // why it cannot be changed here
	Group    string      `json:"group,omitempty"`    // sub-heading inside a card
	Source   Source      `json:"-"`
}

// Card is one task.
type Card struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Sub    string  `json:"sub"`
	Icon   string  `json:"icon"`
	Fields []Field `json:"fields"`
}

func mas(key string) Source  { return Source{Component: "matrixAuthenticationService", Key: key} }
func val(path string) Source { return Source{Values: path} }

func resources(component, name, restarts string) []Field {
	f := func(suffix, label string) Field {
		return Field{
			ID: component + ".resources." + suffix, Label: label, Kind: Quantity, Group: name,
			Restarts: restarts, Source: val(component + ".resources." + suffix),
		}
	}
	return []Field{
		f("requests.memory", "Speicher reserviert"),
		f("limits.memory", "Speicher höchstens"),
		f("requests.cpu", "CPU reserviert"),
		f("limits.cpu", "CPU höchstens"),
	}
}

// Cards is the task layer of etappe 113.
func Cards() []Card {
	host := func(id, path, label, help string) Field {
		return Field{ID: id, Label: label, Help: help, Kind: Text, Source: val(path),
			Restarts: "Die Adresse wird umgestellt; DNS und Zertifikat müssen für die neue Adresse passen.",
			Warn:     "Eine neue Adresse funktioniert erst, wenn ihr DNS-Eintrag auf diesen Server zeigt."}
	}
	var res []Field
	res = append(res, resources("synapse", "Synapse (Homeserver)", "Synapse startet neu (etwa 30 Sekunden keine Nachrichten).")...)
	res = append(res, resources("postgres", "Datenbank (PostgreSQL)", "Die Datenbank startet neu — alle Dienste warten kurz.")...)
	res = append(res, resources("matrixAuthenticationService", "Anmeldung (MAS)", "Die Anmeldung startet neu.")...)

	return []Card{
		{
			ID: "server", Title: "Server & Adressen", Icon: "globe",
			Sub: "Unter welchen Adressen dein Server erreichbar ist",
			Fields: []Field{
				{ID: "serverName", Label: "Servername", Kind: Text, Source: val("serverName"),
					Help:   "Der Teil hinter dem Doppelpunkt in jeder Matrix-ID (@name:servername).",
					Locked: "Lässt sich nach dem ersten Start nicht ändern — alle Accounts, Räume und Nachrichten tragen ihn."},
				host("host.synapse", "synapse.ingress.host", "Homeserver", "Hier verbinden sich Apps und andere Server."),
				host("host.mas", "matrixAuthenticationService.ingress.host", "Anmeldung", "Die Seite, auf der man sich anmeldet und registriert."),
				host("host.element", "elementWeb.ingress.host", "Element Web", "Der Matrix-Client im Browser."),
				host("host.admin", "elementAdmin.ingress.host", "Element Admin", "Die Verwaltungsoberfläche von Element."),
				host("host.rtc", "matrixRTC.ingress.host", "Anrufe", "Über diese Adresse bauen Element Call und die Apps Anrufe auf."),
			},
		},
		{
			ID: "registration", Title: "Registrierung & Anmeldung", Icon: "users",
			Sub: "Wer sich einen Account anlegen darf und wie man sich anmeldet",
			Fields: []Field{
				{ID: "mas.password_registration", Label: "Registrierung mit Benutzername und Passwort", Kind: Bool, Default: false,
					Help:     "An: jeder, der die Anmeldeseite findet, kann sich einen Account anlegen. Aus: Accounts legst du an — oder man registriert sich über einen externen Anbieter (Seite „Anmeldung\").",
					Restarts: "Die Anmeldung startet neu.", Source: mas("account.password_registration_enabled"),
					Warn: "Offene Registrierung ohne Captcha zieht erfahrungsgemäß Spam-Accounts an."},
				{ID: "mas.email_required", Label: "E-Mail-Adresse bei der Registrierung verlangen", Kind: Bool, Default: true,
					Help:     "Wirkt nur bei Registrierung mit Passwort. Braucht einen eingerichteten E-Mail-Versand.",
					Restarts: "Die Anmeldung startet neu.", Source: mas("account.password_registration_email_required")},
				{ID: "mas.passwords", Label: "Anmeldung mit Passwort", Kind: Bool, Default: true,
					Help:     "Aus: Anmeldung nur noch über externe Anbieter (Google, GitHub, Zitadel …).",
					Restarts: "Die Anmeldung startet neu.", Source: mas("passwords.enabled"),
					Warn: "Vorher sicherstellen, dass dein Admin-Account mit einem Anbieter verknüpft ist — sonst kommst du nur noch über den Notzugang herein."},
				{ID: "mas.deactivation", Label: "Nutzer dürfen ihren Account selbst löschen", Kind: Bool, Default: true,
					Restarts: "Die Anmeldung startet neu.", Source: mas("account.account_deactivation_allowed")},
				{ID: "mas.displayname", Label: "Nutzer dürfen ihren Anzeigenamen ändern", Kind: Bool, Default: true,
					Restarts: "Die Anmeldung startet neu.", Source: mas("account.displayname_change_allowed")},
			},
		},
		{
			ID: "resources", Title: "Ressourcen & Kapazität", Icon: "cpu",
			Sub:    "Wie viel Speicher und Rechenzeit jeder Dienst bekommt",
			Fields: res,
		},
	}
}

// Value is a field's current state.
type Value struct {
	Value     interface{} `json:"value"`
	IsDefault bool        `json:"is_default"`
	// SetElsewhere names the additional block that sets this key when it is not ours;
	// the field is then read-only.
	SetElsewhere string `json:"set_elsewhere,omitempty"`
}

// Read resolves every field against the merged settings.
func Read(values map[string]interface{}, fields []Field) map[string]Value {
	out := map[string]Value{}
	for _, f := range fields {
		out[f.ID] = read(values, f)
	}
	return out
}

func read(values map[string]interface{}, f Field) Value {
	if f.Source.Values != "" {
		v, ok := get(values, strings.Split(f.Source.Values, "."))
		if !ok || v == nil {
			return Value{Value: f.Default, IsDefault: true}
		}
		return Value{Value: v}
	}
	blocks := additional(values, f.Source.Component)
	key := strings.Split(f.Source.Key, ".")
	// Another block first: if it sets the key, that is where the value comes from.
	for _, name := range sortedKeys(blocks) {
		if name == Block {
			continue
		}
		if v, ok := get(blocks[name], key); ok {
			return Value{Value: v, SetElsewhere: name}
		}
	}
	if v, ok := get(blocks[Block], key); ok {
		return Value{Value: v}
	}
	return Value{Value: f.Default, IsDefault: true}
}

// additional parses every inline block under <component>.additional. Blocks that come
// from a Secret (`configSecret`) cannot be read here and are left out.
func additional(values map[string]interface{}, component string) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	raw, _ := get(values, []string{component, "additional"})
	m, _ := raw.(map[string]interface{})
	for name, entry := range m {
		e, _ := entry.(map[string]interface{})
		text, _ := e["config"].(string)
		if text == "" {
			continue
		}
		var doc map[string]interface{}
		if yaml.Unmarshal([]byte(text), &doc) == nil {
			out[name] = doc
		}
	}
	return out
}

// Plan is what writing a set of changes means for the section files.
type Plan struct {
	Set    map[string]interface{} // Helm value path → value
	Remove []string               // Helm value paths
}

// Write turns field changes into section-file edits. A nil value means "back to the
// default": the setting is removed, not set to the default — a value written down is a
// value that no longer follows the chart.
func Write(values map[string]interface{}, fields []Field, changes map[string]interface{}) (Plan, error) {
	byID := map[string]Field{}
	for _, f := range fields {
		byID[f.ID] = f
	}
	plan := Plan{Set: map[string]interface{}{}}
	blocks := map[string]map[string]interface{}{} // component → our block, edited
	for id, v := range changes {
		f, ok := byID[id]
		if !ok {
			return Plan{}, fmt.Errorf("unbekannte Einstellung %q", id)
		}
		if f.Locked != "" {
			return Plan{}, fmt.Errorf("%s: %s", f.Label, f.Locked)
		}
		if err := validate(f, v); err != nil {
			return Plan{}, err
		}
		if f.Source.Values != "" {
			if v == nil {
				plan.Remove = append(plan.Remove, f.Source.Values)
			} else {
				plan.Set[f.Source.Values] = v
			}
			continue
		}
		if cur := read(values, f); cur.SetElsewhere != "" {
			return Plan{}, fmt.Errorf("%s wird im Block %q gesetzt — dort ändern oder ihn entfernen", f.Label, cur.SetElsewhere)
		}
		b, ok := blocks[f.Source.Component]
		if !ok {
			b = copyMap(additional(values, f.Source.Component)[Block])
			blocks[f.Source.Component] = b
		}
		key := strings.Split(f.Source.Key, ".")
		if v == nil {
			del(b, key)
		} else {
			set(b, key, v)
		}
	}
	for component, b := range blocks {
		path := component + ".additional." + Block
		if len(b) == 0 {
			plan.Remove = append(plan.Remove, path)
			continue
		}
		// Two-space indent, like every other line of the section files.
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(b); err != nil {
			return Plan{}, err
		}
		_ = enc.Close()
		plan.Set[path+".config"] = buf.String()
	}
	sort.Strings(plan.Remove)
	return plan, nil
}

func validate(f Field, v interface{}) error {
	if v == nil {
		return nil
	}
	switch f.Kind {
	case Bool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: an oder aus erwartet", f.Label)
		}
	case Text:
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" || strings.ContainsAny(s, " /:") {
			return fmt.Errorf("%s: eine Adresse wie matrix.example.org erwartet", f.Label)
		}
	case Quantity:
		s, ok := v.(string)
		if !ok || !quantityRE(s) {
			return fmt.Errorf("%s: eine Menge wie 1Gi, 512Mi oder 500m erwartet", f.Label)
		}
	}
	return nil
}

func quantityRE(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	if i == 0 {
		return false
	}
	switch s[i:] {
	case "", "m", "k", "M", "G", "T", "Ki", "Mi", "Gi", "Ti":
		return true
	}
	return false
}

func get(m interface{}, path []string) (interface{}, bool) {
	cur := m
	for _, k := range path {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur, ok = mm[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func set(m map[string]interface{}, path []string, v interface{}) {
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			m[k] = next
		}
		m = next
	}
	m[path[len(path)-1]] = v
}

func del(m map[string]interface{}, path []string) {
	if len(path) == 1 {
		delete(m, path[0])
		return
	}
	next, ok := m[path[0]].(map[string]interface{})
	if !ok {
		return
	}
	del(next, path[1:])
	if len(next) == 0 {
		delete(m, path[0])
	}
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range m {
		if sub, ok := v.(map[string]interface{}); ok {
			out[k] = copyMap(sub)
		} else {
			out[k] = v
		}
	}
	return out
}

func sortedKeys(m map[string]map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AllFields flattens the cards.
func AllFields(cards []Card) []Field {
	var out []Field
	for _, c := range cards {
		out = append(out, c.Fields...)
	}
	return out
}
