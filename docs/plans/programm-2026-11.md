# Programm November 2026 — nach der Selbstkritik

Stand 2026-11-11. Betreiber-Auftrag: „mach alles davon" (Selbstkritik vom selben Tag).
Dieses Dokument ist die Reihenfolge und der Zuschnitt; jedes Paket bekommt vor dem ersten
Code seinen eigenen Plan `etappe-NNN-*.md` (PROZESS §1). Stand jeweils in `HANDOFF.md`.

## Befund in einem Absatz

Es wurde schneller gebaut als in Produktion bewiesen. Die schlimmsten Fehler fand der
Betreiber per Screenshot, kein Test: es gibt keinen Test, der die Oberfläche gegen einen
echten Cluster durchklickt. 126 Releases, gut die Hälfte der letzten vierzehn Reparaturen.
Oberfläche gewachsen statt gestaltet; Doku für den Assistenten, nicht für Menschen; nur
Deutsch; Zuschnitt (ein Knoten, k3s, Traefik, Cloudflare) nirgends ehrlich benannt.

## Pakete, in dieser Reihenfolge

| # | Paket | Fertig wenn |
|---|---|---|
| A | **E120 Backups abnehmen und veröffentlichen** (liegt gebaut, ungepusht) | Abnahme auf dem Testcluster gegen echten Bucket: Zeitplan-Lauf, Restore aus Bucket, Upgrade mit Vorab-Backup, Upgrade mit falschem Zugang sperrt; Release |
| B | **E121 Oberflächen-Test gegen den Testcluster** | Ein Lauf meldet an, öffnet jede Seite, ändert eine Einstellung, übernimmt; läuft per `make e2e`; fällt nachweislich, wenn eine Seite bricht |
| C | **E122 Release-Takt**: `edge`/`stable`, Tag erst nach Abnahme | Update-Prüfung kennt den Kanal; RELEASING.md beschreibt die Abnahme; ein Release geht den neuen Weg |
| D | **E123 Benachrichtigungen** (Webhook in Admin-Raum, E-Mail) | DNS-Ausfall, Zertifikat < 14 Tage, Update, hängender Pod, Backup gescheitert lösen je eine Nachricht aus und eine Entwarnung; Testnachricht-Knopf |
| E | **E124 Menü, Texte, globale Warnleiste** | Keine Phasen-Tags mehr; Warnleiste auf jeder Seite (DNS, ESS failed, Update, Notzugang); „Was ist neu" nach Update |
| F | **E125 Einladungs-Codes** (MAS-Registrierungstokens) | Anlegen, Ablauf, Nutzungslimit, widerrufen; Registrierung „nur mit Code" als Einstellung |
| G | **E126 Speicher: Blick + Aufräumen** | Wachstum Medien/DB, Prognose; fremden Medien-Cache und alte Historie bereinigen mit Vorschau |
| H | **E127 Server-Mitteilung an alle; Räume löschen** | Beides mit Bestätigung und Audit-Eintrag |
| I | **E128 Bridge aus dem Panel anmelden; Schalter Absender-Adressen; Hooks auslaufen** | Registrierung einfügen → Secret → `synapse.appservices`; Aufgaben-Feld mit mehreren Pfaden; Hooks-Seite entfällt, wenn nichts mehr zu tun ist |
| J | **E129 Diagnosepaket, Zeitstrahl, Suche** | Ein-Klick-Archiv ohne Geheimnisse; Upgrades/Änderungen/Ausfälle auf einer Achse; ⌘K über Seiten, Einstellungen, Nutzer, Räume |
| K | **E130 README für Menschen + Unterstützungs-Matrix** | Screenshots, „in 10 Minuten", ehrlich: getestet / ungetestet |
| L | **E131 Englische Oberfläche** | Alle sichtbaren Texte über einen Katalog; Sprache wählbar; ein Test fällt bei fehlendem Schlüssel |
| M | **Rechte prüfen**: Rollen-Abstufung, 2FA Notzugang | Eigene Sicherheits-Durchsicht, Ergebnis in BACKLOG |

## Zuschnitt-Entscheidungen (Betreiber kann widersprechen)

- Phase 6 „mehrere ESS-Instanzen" wird zurückgestellt: erst ein Server, der nicht stirbt.
- Menüpunkt-Gruppe „Compliance" entfällt, bis es ein übergreifendes Audit gibt.
- Hooks laufen aus (Paket I), statt weiter erklärt zu werden.

## Arbeitsweise

Orchestrator-Modus (ORCHESTRATOR.md): Lesen → `scout`, Umsetzen → `worker`, Entscheidungen,
Abnahme und Release im Hauptkontext. Nach jedem Paket: HANDOFF.md, dann dem Betreiber
`/clear` vorschlagen. Produktion bleibt für den Assistenten lesend.
