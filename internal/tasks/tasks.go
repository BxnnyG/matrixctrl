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
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Block is the additional-config key MatrixCtrl owns in each component.
const Block = "matrixctrl-tasks"

// Kind is how a field is edited.
type Kind string

const (
	Bool      Kind = "bool"
	Text      Kind = "text"     // a host name
	Quantity  Kind = "quantity" // Kubernetes quantity: 1Gi, 500m
	Size      Kind = "size"     // Synapse size: 100M, 512K
	Duration  Kind = "duration" // Synapse duration: 7d, 12h, 1y
	Port      Kind = "port"
	Choice    Kind = "choice"
	AllowList Kind = "allowlist" // nil = everyone, [] = no one, [a, b] = only these
	Label     Kind = "label"     // free text: a name shown to people
)

// Source says where a value lives.
type Source struct {
	// Values is a dotted Helm value path. Exclusive with Component/Key.
	Values string
	// Component is the chart section whose additional config holds Key
	// ("matrixAuthenticationService", "synapse").
	Component string
	Key       string // dotted key inside that service's configuration
	// JSON marks a component whose additional blocks are JSON text directly under the
	// key (Element Web), rather than YAML under `.config` (MAS, Synapse).
	JSON bool
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
	Options  []Option    `json:"options,omitempty"`  // for Choice
	Source   Source      `json:"-"`
}

// Option is one choice of a Choice field.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Card is one task.
type Card struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Sub    string  `json:"sub"`
	Icon   string  `json:"icon"`
	Fields []Field `json:"fields"`
}

func mas(key string) Source     { return Source{Component: "matrixAuthenticationService", Key: key} }
func synapse(key string) Source { return Source{Component: "synapse", Key: key} }
func web(key string) Source     { return Source{Component: "elementWeb", Key: key, JSON: true} }
func val(path string) Source    { return Source{Values: path} }

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

const (
	synapseRestart = "Synapse startet neu (etwa 30 Sekunden keine Nachrichten)."
	callsRestart   = "Der Anruf-Server startet neu — laufende Anrufe brechen ab."
	webRestart     = "Element Web wird neu ausgeliefert; Nutzer sehen es nach dem Neuladen."
	portWarn       = "Der neue Port muss in der Firewall deines Servers (und beim Hoster) offen sein."
)

// Cards is the task layer of etappes 113 and 114.
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
			ID: "messages", Title: "Nachrichten & Medien", Icon: "file",
			Sub: "Dateien, Link-Vorschauen und wie lange etwas aufbewahrt wird",
			Fields: []Field{
				{ID: "synapse.maxUpload", Label: "Größte Datei, die man hochladen kann", Kind: Size, Default: "100M",
					Help:     "Bilder, Videos und Dateien in Nachrichten. Angabe mit M (Megabyte) oder K (Kilobyte).",
					Restarts: synapseRestart, Source: val("synapse.media.maxUploadSize"),
					Warn: "Sehr große Uploads brauchen auch Platz im Zwischenspeicher von Synapse und können von einem Proxy davor begrenzt werden."},
				{ID: "synapse.urlPreviews", Label: "Link-Vorschauen", Kind: Bool, Default: true,
					Help:     "Links in Nachrichten zeigen Titel und Bild der Seite. Dein Server ruft die Seite dafür ab — interne Adressen sind gesperrt.",
					Restarts: synapseRestart, Source: synapse("url_preview_enabled")},
				{ID: "synapse.remoteMedia", Label: "Medien anderer Server zwischenspeichern für", Kind: Duration,
					Help:     "Bilder und Dateien aus Räumen anderer Server werden lokal zwischengespeichert. Leer: unbegrenzt. Z. B. 90d — danach werden sie bei Bedarf neu geladen.",
					Restarts: synapseRestart, Source: synapse("media_retention.remote_media_lifetime")},
				{ID: "synapse.redactionRetention", Label: "Gelöschte Nachrichten noch aufbewahren für", Kind: Duration, Default: "7d",
					Help:     "So lange können Moderatoren den Inhalt gelöschter Nachrichten noch einsehen, danach ist er endgültig weg.",
					Restarts: synapseRestart, Source: synapse("redaction_retention_period")},
			},
		},
		{
			ID: "federation", Title: "Föderation", Icon: "globe",
			Sub: "Mit welchen anderen Matrix-Servern dein Server spricht",
			Fields: []Field{
				{ID: "synapse.federationAllow", Label: "Verbindung zu anderen Servern", Kind: AllowList,
					Help:     "Alle: deine Nutzer können mit jedem Matrix-Server schreiben (Standard). Keine: nur untereinander. Oder nur mit bestimmten Servern.",
					Restarts: synapseRestart, Source: synapse("federation_domain_whitelist"),
					Warn: "Eingeschränkt verlieren Räume mit Mitgliedern auf anderen Servern die Verbindung zu ihnen."},
				{ID: "synapse.publicRoomsFederation", Label: "Andere Server dürfen dein öffentliches Raumverzeichnis sehen", Kind: Bool, Default: false,
					Restarts: synapseRestart, Source: synapse("allow_public_rooms_over_federation")},
			},
		},
		{
			ID: "calls", Title: "Anrufe", Icon: "phone",
			Sub: "Sprach- und Videoanrufe mit Element Call",
			Fields: []Field{
				{ID: "rtc.enabled", Label: "Anrufe mit Element Call", Kind: Bool, Default: true,
					Help: "Die Anruf-Dienste (SFU und Anruf-Anmeldung) laufen auf deinem Server.", Source: val("matrixRTC.enabled"),
					Restarts: "Die Anruf-Dienste werden gestartet oder entfernt."},
				{ID: "rtc.hostNetwork", Label: "Anruf-Server direkt ans Netz (empfohlen)", Kind: Bool, Default: false,
					Help:     "Der Anruf-Server lauscht direkt auf den Ports des Servers. Nötig, damit Anrufe zwischen verschiedenen Netzen (Mobilfunk ↔ Glasfaser) zuverlässig klappen.",
					Restarts: callsRestart, Source: val("matrixRTC.sfu.hostNetwork")},
				{ID: "rtc.turn", Label: "TURN-Relais", Kind: Bool, Default: false,
					Help:     "Leitet Anrufe über den Server, wenn ein Netz direkte Verbindungen blockiert (Firmen-WLAN, Hotels).",
					Restarts: callsRestart, Source: val("matrixRTC.sfu.exposedServices.turn.enabled")},
				{ID: "rtc.portTcp", Label: "Port für Anrufe (TCP)", Kind: Port, Default: 30001,
					Restarts: callsRestart, Source: val("matrixRTC.sfu.exposedServices.rtcTcp.port"), Warn: portWarn},
				{ID: "rtc.portUdp", Label: "Port für Anrufe (UDP)", Kind: Port, Default: 30002,
					Restarts: callsRestart, Source: val("matrixRTC.sfu.exposedServices.rtcMuxedUdp.port"), Warn: portWarn},
				{ID: "rtc.portTurn", Label: "Port für TURN (UDP)", Kind: Port, Default: 30004,
					Restarts: callsRestart, Source: val("matrixRTC.sfu.exposedServices.turn.port"), Warn: portWarn},
			},
		},
		{
			ID: "appearance", Title: "Aussehen", Icon: "sparkle",
			Sub: "Wie Element Web für deine Nutzer aussieht",
			Fields: []Field{
				{ID: "web.brand", Label: "Name der App", Kind: Label, Default: "Element",
					Help: "Steht im Browser-Tab und in Element Web statt „Element\".", Restarts: webRestart, Source: web("brand")},
				{ID: "web.theme", Label: "Farbschema für neue Nutzer", Kind: Choice, Default: "light",
					Options:  []Option{{"light", "Hell"}, {"dark", "Dunkel"}},
					Restarts: webRestart, Source: web("default_theme")},
			},
		},
		{
			ID: "resources", Title: "Ressourcen & Kapazität", Icon: "cpu",
			Sub:    "Wie viel Speicher und Rechenzeit jeder Dienst bekommt",
			Fields: res,
		},
		{
			ID: "workers", Title: "Synapse-Worker", Icon: "activity",
			Sub:    "Teile von Synapses Arbeit in eigene Prozesse auslagern — ob es sich lohnt, sagt Worker-Insights",
			Fields: workerFields(),
		},
	}
}

// workerFields are the ESS worker types Worker-Insights can recommend (etappe 118), one
// switch each. Not all 23: the rest serve setups this product does not build (appservice
// bridges, SSO) or exist for scale far past a single node, and a list of switches nobody
// can judge is the opposite of a task.
func workerFields() []Field {
	const restarts = "Synapse startet neu; der Worker kommt als eigener Pod dazu."
	const warn = "Jeder Worker ist ein eigener Synapse-Prozess — rechne mit 100–300 MB Speicher. Die Vorschau zeigt, ob der Knoten das hergibt."
	w := func(typ, label, help, group string) Field {
		return Field{ID: "synapse.workers." + typ, Label: label, Help: help, Kind: Bool, Default: false, Group: group,
			Restarts: restarts, Warn: warn, Source: val("synapse.workers." + typ + ".enabled")}
	}
	return []Field{
		w("synchrotron", "Sync", "Beantwortet das ständige Nachfragen der Apps nach Neuigkeiten — bei vielen gleichzeitig verbundenen Geräten die größte Last.", "Apps"),
		w("sliding-sync", "Sliding Sync", "Dasselbe für die neuen Element-Apps (Element X).", "Apps"),
		w("client-reader", "Lesen", "Verlauf, Mitgliederlisten, Profile.", "Apps"),
		w("event-creator", "Senden", "Nachrichten und Raumänderungen annehmen.", "Apps"),
		w("encryption", "Verschlüsselung", "Schlüsselaustausch zwischen Geräten.", "Apps"),
		w("media-repository", "Medien", "Hochladen, Herunterladen, Vorschaubilder, Link-Vorschauen.", "Apps"),
		w("federation-inbound", "Föderation empfangen", "Nimmt an, was andere Server schicken.", "Föderation"),
		w("federation-reader", "Föderation beantworten", "Beantwortet Abfragen anderer Server: Schlüssel, Geräte, Raumzustand.", "Föderation"),
		w("federation-sender", "Föderation senden", "Schickt an andere Server — hilft, wenn deren Warteschlange wächst.", "Föderation"),
		w("pusher", "Push", "Benachrichtigungen an Handys.", "Sonstiges"),
		w("background", "Hintergrundaufgaben", "Aufräumen, Statistiken, Zähler.", "Sonstiges"),
		w("receipts", "Lesebestätigungen", "", "Sonstiges"),
		w("typing-persister", "Tipp-Anzeige", "", "Sonstiges"),
		w("account-data", "Kontodaten", "Einstellungen, die Apps auf dem Server ablegen.", "Sonstiges"),
		w("user-dir", "Nutzerverzeichnis", "Die Suche nach Personen.", "Sonstiges"),
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
		// Installations seeded from the chart's complete values carry nearly every
		// default written out. A value equal to the default is the default: badge
		// "Standard", nothing to reset.
		return Value{Value: v, IsDefault: f.Default != nil && fmt.Sprint(v) == fmt.Sprint(f.Default)}
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

// additional parses every inline block under <component>.additional: YAML under
// `.config` (MAS, Synapse) or JSON text directly (Element Web — YAML is a superset of
// JSON, so one parser reads both). Blocks that come from a Secret (`configSecret`)
// cannot be read here and are left out.
func additional(values map[string]interface{}, component string) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	raw, _ := get(values, []string{component, "additional"})
	m, _ := raw.(map[string]interface{})
	for name, entry := range m {
		var text string
		switch e := entry.(type) {
		case string:
			text = e
		case map[string]interface{}:
			text, _ = e["config"].(string)
		}
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
	isJSON := map[string]bool{}
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
		if n, ok := v.(float64); ok && f.Kind == Port {
			v = int(n)
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
		isJSON[f.Source.Component] = f.Source.JSON
		b, ok := blocks[f.Source.Component]
		if !ok {
			b = copyMap(additional(values, f.Source.Component)[Block])
			blocks[f.Source.Component] = b
		}
		key := strings.Split(f.Source.Key, ".")
		// Our block holds only what differs from the service's default. Switching a
		// setting on and back off must leave nothing behind — not a written-out default
		// that keeps the pending-changes bar up for a change that changes nothing.
		if v != nil && f.Default != nil && fmt.Sprint(v) == fmt.Sprint(f.Default) {
			v = nil
		}
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
		if isJSON[component] {
			text, err := json.Marshal(b)
			if err != nil {
				return Plan{}, err
			}
			plan.Set[path] = string(text)
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
	case Size:
		if s, ok := v.(string); !ok || !sizeRE.MatchString(s) {
			return fmt.Errorf("%s: eine Größe wie 100M oder 512K erwartet", f.Label)
		}
	case Duration:
		if s, ok := v.(string); !ok || !durationRE.MatchString(s) {
			return fmt.Errorf("%s: eine Dauer wie 7d, 12h oder 1y erwartet", f.Label)
		}
	case Port:
		n, ok := v.(float64) // JSON numbers
		if i, isInt := v.(int); isInt {
			n, ok = float64(i), true
		}
		if !ok || n != float64(int(n)) || n < 1 || n > 65535 {
			return fmt.Errorf("%s: ein Port zwischen 1 und 65535 erwartet", f.Label)
		}
	case Choice:
		s, _ := v.(string)
		for _, o := range f.Options {
			if o.Value == s {
				return nil
			}
		}
		return fmt.Errorf("%s: unbekannte Auswahl %v", f.Label, v)
	case Label:
		if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" || len(s) > 64 {
			return fmt.Errorf("%s: ein Name mit 1 bis 64 Zeichen erwartet", f.Label)
		}
	case AllowList:
		list, ok := v.([]interface{})
		if !ok {
			return fmt.Errorf("%s: eine Liste von Servern erwartet", f.Label)
		}
		for _, x := range list {
			if s, ok := x.(string); !ok || !hostRE.MatchString(s) {
				return fmt.Errorf("%s: %v ist kein Servername", f.Label, x)
			}
		}
	}
	return nil
}

var (
	sizeRE     = regexp.MustCompile(`^[0-9]+[KMG]$`)
	durationRE = regexp.MustCompile(`^[0-9]+[smhdwy]$`)
	hostRE     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+(:[0-9]{1,5})?$`)
)

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
