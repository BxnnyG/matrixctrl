# HANDOFF

## Ziel
MatrixCtrl: Day-2-Admin-Schicht für ESS. Alle Roadmap-Seiten (Phase 1–5) sind gebaut; jetzt
Lücken schließen und Betreiber-Bedürfnisse (siehe „Offen").

## Erledigt (Stand 0.1.127 + E121, 2026-11-16)
- 116–116d: Update aus dem Panel (Job, atomic), Update-Karte + „Jetzt prüfen", Matrix-Login nach Umzug, Notzugang-Hinweise.
- 117 Föderation · 118 Worker-Insights · 119 Bridges (Hookshot + Appservices).
- 119a DNS-Überwachung · 119b Hooks überspringen, wenn das Chart sie abdeckt · 119c SFU-Port-Blockade beim Upgrade · 119d MAS-Sitzungen aufräumen.
- 0.1.126: Hookshot-Anleitung nach erstem echten Durchlauf (Gruppenraum, Moderator, Adresse im Direktchat). Webhook Ende-zu-Ende auf dem Testcluster bestätigt.
- Orchestrator-Modus eingerichtet (ORCHESTRATOR.md, .claude/agents/).
- **B erledigt: E121 `make e2e`** (kein Release nötig, nur Werkzeug): Login übers Formular, alle Routen aus dem Router (`web/scripts/lib/routes.mjs`, auch für verify-ui), „Name der App" übernehmen → Helm-Revision +1 → zurück. Grün auf dem Testcluster (11 min); vier Gegenproben rot.

## Offen / Nächster Schritt
**Maßgeblich: `docs/plans/programm-2026-11.md`** (Pakete A–M in fester Reihenfolge, Betreiber-Auftrag 2026-11-11).
- A (E120 Backups, v0.1.127) und B (E121 `make e2e`) erledigt. Offen aus A: Zeitplan läuft im Panel-Prozess (verpasster Lauf wird nicht nachgeholt); keine Vorschau direkt aus dem Bucket; ungeklärt: ESS-Upgrade startete zweimal kurz nacheinander ohne 409.
- **Als Nächstes C: E122 Release-Takt** (`edge`/`stable`, Tag erst nach Abnahme), dann D … siehe Programm.
- Testcluster: matrixctrl 0.1.127, OIDC an. Backup-Ziel der Spielwiese ist der S3-Dienst in Namespace `s3test` (`e120-s3` gelöscht 2026-11-16). `make e2e` braucht OIDC aus (Helm-Wert `oidc.enabled=false`, danach wieder an) und einen leeren `config/diff`.
- Öffentliche Namen der Spielwiesen-Ingresses führen nicht zwingend zur Spielwiese: im Test immer cluster-intern prüfen (port-forward), nie über den öffentlichen Namen.
- Bekannt aus E121: Die Fehlermeldung bei falschem Passwort im Login-Formular zeigt nicht die Servermeldung.
Noch nicht in Produktion bewiesen: Port-Blockade-Auflösung (119c), MAS-Sitzungs-Aufräumen (119d), Föderations-Gegenstellen.

## Entscheidungen (mit Kurzbegründung)
- Hooks abgelöst statt für alle eingeschaltet — ESS setzt hostNetwork/ETP selbst (§4.125).
- Git-Historie nicht umgeschrieben (ID-Präfix in v0.1.123-Test): würde zwei Release-Tags versetzen und Versionen neu veröffentlichen; GitHub behält den Commit ohnehin.
- Worker-Urteil am 95. Perzentil einer Woche, nicht am Schnitt (§4.123).
- MatrixCtrl betreibt fremde Bridges nicht, es meldet sie nur an/zeigt sie (§4.128).
- e2e ändert „Name der App" (Betreiber 2026-11-14): nur Element Web startet neu. Der Lauf verweigert, wenn fremde Änderungen anstehen — nie blind verwerfen: auf der Spielwiese war darunter eine nötige CPU-Absenkung.

## Relevante Dateien
docs/ROADMAP.md (Etappen-Tabelle) · docs/DESIGN.md §4.119–4.128 · CHANGELOG.md · docs/plans/etappe-1*.md ·
internal/{federation,synmetrics,dnshealth,bridges,ociregistry}/ · internal/mas/sessions.go · internal/k8s/hostport.go ·
web/scripts/{e2e.mjs,verify-ui.mjs,lib/}
