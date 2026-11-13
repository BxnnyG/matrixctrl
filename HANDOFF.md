# HANDOFF

## Ziel
MatrixCtrl: Day-2-Admin-Schicht für ESS. Alle Roadmap-Seiten (Phase 1–5) sind gebaut; jetzt
Lücken schließen und Betreiber-Bedürfnisse (siehe „Offen").

## Erledigt (Stand 0.1.127, 2026-11-12)
- 116–116d: Update aus dem Panel (Job, atomic), Update-Karte + „Jetzt prüfen", Matrix-Login nach Umzug, Notzugang-Hinweise.
- 117 Föderation · 118 Worker-Insights · 119 Bridges (Hookshot + Appservices).
- 119a DNS-Überwachung · 119b Hooks überspringen, wenn das Chart sie abdeckt · 119c SFU-Port-Blockade beim Upgrade · 119d MAS-Sitzungen aufräumen.
- 0.1.126: Hookshot-Anleitung nach erstem echten Durchlauf (Gruppenraum, Moderator, Adresse im Direktchat). Webhook Ende-zu-Ende auf dem Testcluster bestätigt.
- Orchestrator-Modus eingerichtet (ORCHESTRATOR.md, .claude/agents/).

## Offen / Nächster Schritt
**Maßgeblich: `docs/plans/programm-2026-11.md`** (Pakete A–M in fester Reihenfolge, Betreiber-Auftrag 2026-11-11).
- **A erledigt: E120 Backups, v0.1.127.** Abnahme auf dem Testcluster bestanden (Probe, Lauf, Entschlüsseln mit Gegenprobe, Zeitplan, Upgrade sperrt bei falschem Zugang, Restore-Vorschau; kein echter Restore). Dabei behoben: manuelles Backup wurde vom Zeitplan-Lauf desselben Tages gelöscht → jetzt 30 Tage wie Vor-Upgrade; Aufbewahrung protokolliert. Offen daraus: Zeitplan läuft im Panel-Prozess (verpasster Lauf wird nicht nachgeholt); keine Vorschau direkt aus dem Bucket; ungeklärt: ESS-Upgrade startete zweimal kurz nacheinander ohne 409.
- Testcluster-Zustand: matrixctrl läuft mit lokalem Image 0.1.127-e120 (Helm rev 98) — beim nächsten Release per Selbst-Update-Test auf das veröffentlichte Image bringen. S3-Testdienst in Namespace `e120-s3`. Sitzung ohne Browser: OIDC per Helm-Wert aus → Notzugang → danach wieder an (so für Paket B nutzbar).
- **Als Nächstes B: E121 Oberflächen-Test gegen den Testcluster.**
- Danach B (E121 Oberflächen-Test), C (Release-Takt), D (Benachrichtigungen) … siehe Programm.
Noch nicht in Produktion bewiesen: Port-Blockade-Auflösung (119c), MAS-Sitzungs-Aufräumen (119d), Föderations-Gegenstellen.

## Entscheidungen (mit Kurzbegründung)
- Hooks abgelöst statt für alle eingeschaltet — ESS setzt hostNetwork/ETP selbst (§4.125).
- Git-Historie nicht umgeschrieben (ID-Präfix in v0.1.123-Test): würde zwei Release-Tags versetzen und Versionen neu veröffentlichen; GitHub behält den Commit ohnehin.
- Worker-Urteil am 95. Perzentil einer Woche, nicht am Schnitt (§4.123).
- MatrixCtrl betreibt fremde Bridges nicht, es meldet sie nur an/zeigt sie (§4.128).

## Relevante Dateien
docs/ROADMAP.md (Etappen-Tabelle) · docs/DESIGN.md §4.119–4.128 · CHANGELOG.md · docs/plans/etappe-1*.md ·
internal/{federation,synmetrics,dnshealth,bridges,ociregistry}/ · internal/mas/sessions.go · internal/k8s/hostport.go
