# Etappe 124 — Menü, Texte, globale Warnleiste, „Was ist neu"

Stand 2026-11-18. Paket E aus `programm-2026-11.md`. Heute zeigt das Menü Roadmap-Phasen
(„P3", „P4", „P5"), hat zwei Gruppen „Betrieb" und eine Gruppe „Compliance" mit einer Seite,
die nichts mit Compliance zu tun hat. Probleme (DNS-Ausfall, ESS-Release `failed`) sieht der
Betreiber nur auf dem Dashboard oder gar nicht. Nach einem Update erfährt er nicht, was neu ist.

## Befund (gelesen)

- Menü: `web/src/components/layout/Shell.tsx` (`NAV` Z.25–62, `TITLES` Z.63–78), Phasen-Badge
  Z.143/147, Schalter `showPhaseTags` in `Tweaks.tsx:73`, `lib/theme.ts:17/34`.
  Text „Phase 1.5:" in `routes/setup.tsx:155`.
- Keine gemeinsame Banner-Komponente. Einzelstücke: `ClusterDNSAlert` (nur Dashboard),
  `MatrixLoginMissing`/`EmergencySessionNeedsMatrix`, Update-Pill in der Sidebar.
- Signale: `internal/notify` hat Prüfer DNS/Zertifikat/Update/Pod/Backup, prüft aber nur
  eingeschaltete Auslöser und hält kein „aktuell bestehend" im Speicher. ESS-Release-Status
  (`helm/history.go`, `/api/v1/status` Feld `release`) wird von keinem Prüfer gelesen.
  Notzugang = Sitzung des Bootstrap-Nutzers (`auth.BootstrapUserID`, `/api/v1/auth/me`).
- CHANGELOG.md wird nicht eingebettet; Version über `GET /api/v1/version`.

## Betreiber-Entscheidungen (2026-11-18)

1. Menü **nach Aufgaben**:
   Übersicht (Dashboard) · Server (Einstellungen, Anmeldung, E-Mail, Calls) ·
   Menschen (Benutzer, Räume, Moderation) · Vernetzung (Föderation, Bridges) ·
   Betrieb (System, Auslastung [= Worker-Insights, Route bleibt `/workers`], TLS & DNS,
   Backup, Benachrichtigungen) · Wartung (Updates, Audit-Log, Setup, Hooks).
2. „Was ist neu" zeigt den **CHANGELOG-Abschnitt** der installierten Version(en) unverändert.

## Was gebaut wird

1. **notify: aktueller Zustand unabhängig vom Versand.** `Engine.Once` lässt *alle* Prüfer
   laufen (vor dem Laden der Einstellungen, damit ein DB-Fehler die Leiste nicht leert) und
   hält das Ergebnis je Auslöser im Speicher (`Engine.Current()`: Alerts + Prüfzeit + Auslöser
   mit Prüffehler). Der Auslöser-Schalter steuert weiterhin nur Versand und `notify_state`.
2. **Neuer Prüfer `release`** (schließt „Offen aus D"): ESS-Helm-Release `failed` → Alert;
   `pending-*` länger als 10 min → Alert; kein Release (Greenfield) → nichts; Lesefehler →
   unbekannt, kein Alert. Neuer Auslöser `release`, standardmäßig an. `hooks-failed` ist kein
   Release-Fehler (PROZESS Fall 8) — nicht in dieser Etappe.
3. **API `GET /api/v1/alerts`** (jede angemeldete Rolle, beide Anmeldearten): aus
   `Current()` je Alert `key, trigger, severity (error|warning|info), title, detail, link`
   (Link auf die zuständige Seite) + `checked_at`, `unknown`.
4. **Globale Warnleiste** `web/src/components/GlobalAlerts.tsx`, in `Shell` über dem Inhalt,
   auf jeder Seite; fragt alle 60 s. Notzugang wird clientseitig aus der Sitzung ergänzt.
   Update-Hinweis (info) ist je Version wegklickbar (localStorage, try/catch); Probleme nicht.
   Backend nicht erreichbar → Leiste sagt das in einer Zeile, statt zu verschwinden.
5. **Menü/Texte:** `NAV` neu, Phasen-Badges, `showPhaseTags` und „Phase 1.5:" entfernt;
   Seitenuntertitel ohne Roadmap-/Englisch-Jargon („Onboarding · Deploy · Adopt", „mit
   Patch-erhaltenden Hooks").
6. **„Was ist neu":** CHANGELOG.md per `go:embed`, `GET /api/v1/whatsnew?since=X.Y.Z` liefert
   die Abschnitte `(since, current]` (rc-Versionen zählen zur Zielversion). Frontend merkt sich
   die zuletzt gesehene Version (localStorage); erster Besuch merkt nur, zeigt nichts. Dialog
   nach Update, wieder aufrufbar über die Versionsanzeige in der Sidebar. Kleiner
   Markdown-Renderer (Überschriften, Listen, fett, Code, Links) ohne neue Abhängigkeit.

## Randfälle (PROZESS §1)

1 Greenfield/anderer Namespace: `release`-Prüfer nimmt den entdeckten Release, ohne Release
nichts · 2 `failed`/`pending-upgrade`: genau das meldet der Prüfer · 4 Cluster langsam: jeder
Prüfer mit Timeout, Fehler = „unbekannt", nie ein falscher Alarm · 5 kein Internet: Update-
Prüfer liefert dann nichts, Leiste bleibt · 6 beide Anmeldearten: `/alerts` und `/whatsnew` in
beiden erreichbar · 3/7/8: nicht berührt bzw. `hooks-failed` bewusst ausgeklammert.

## Fertig wenn

Go-Tests für `Current()` (auch bei ausgeschaltetem Auslöser), `release`-Prüfer (failed,
pending kurz/lang, kein Release, Fehler) und CHANGELOG-Ausschnitt (rc, übersprungene
Versionen, unbekannte Version); vitest für Leiste und Menü (kein „P3"/„Compliance");
`make check` grün; Gegenproben rot; rc auf dem Testcluster: Leiste auf jeder Seite
(DNS-Ausfall/Release-Fehler simuliert), „Was ist neu" nach Self-Update; `make e2e` + S11.
Release v0.1.130 über rc.
