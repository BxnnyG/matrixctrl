# HANDOFF

## Ziel
MatrixCtrl: Day-2-Admin-Schicht für ESS. Alle Roadmap-Seiten (Phase 1–5) sind gebaut; jetzt
Lücken schließen und Betreiber-Bedürfnisse (siehe „Offen").

## Erledigt (Stand 0.1.129, 2026-11-17)
- 116–116d: Update aus dem Panel (Job, atomic), Update-Karte + „Jetzt prüfen", Matrix-Login nach Umzug, Notzugang-Hinweise.
- 117 Föderation · 118 Worker-Insights · 119 Bridges (Hookshot + Appservices).
- 119a DNS-Überwachung · 119b Hooks überspringen, wenn das Chart sie abdeckt · 119c SFU-Port-Blockade beim Upgrade · 119d MAS-Sitzungen aufräumen.
- 0.1.126: Hookshot-Anleitung nach erstem echten Durchlauf (Gruppenraum, Moderator, Adresse im Direktchat). Webhook Ende-zu-Ende auf dem Testcluster bestätigt.
- **C erledigt: E122 Release-Takt, v0.1.128.** `vX.Y.Z-rc.N` → `:edge`, nach Abnahme `vX.Y.Z` vom selben Commit (docs/RELEASING.md). Erster Durchlauf komplett: rc.1 → Testcluster → e2e/S11 → 0.1.128 per Self-Update, gleicher Commit.
- **D erledigt: E123 Benachrichtigungen, v0.1.129** (rc.1 → rc.2 → stable, gleicher Commit wie rc.2). Webhook + E-Mail (MAS-SMTP oder eigener Zugang), Erinnerung aus/6 h/24 h, Zustellung je Kanal (Migration 021). DNS/Backup/Pod live abgenommen, Zertifikat/Update per Test.
- **E erledigt: E124 Menü/Warnleiste/Was ist neu, v0.1.130** (rc.1 → stable, gleicher Commit, 2026-11-18). Menü nach Aufgaben, „Was ist neu" = CHANGELOG eingebettet (beides Betreiber). notify prüft immer alle Auslöser (`Engine.Current()` → `GET /api/v1/alerts`), neuer Auslöser `release`. vitest-Wartezeit 5 s (Flake unter Last).
- Orchestrator-Modus eingerichtet (ORCHESTRATOR.md, .claude/agents/).
- **B erledigt: E121 `make e2e`** (kein Release nötig, nur Werkzeug): Login übers Formular, alle Routen aus dem Router (`web/scripts/lib/routes.mjs`, auch für verify-ui), „Name der App" übernehmen → Helm-Revision +1 → zurück. Grün auf dem Testcluster (11 min); vier Gegenproben rot.

## Offen / Nächster Schritt
**Maßgeblich: `docs/plans/programm-2026-11.md`** (Pakete A–M in fester Reihenfolge, Betreiber-Auftrag 2026-11-11).
- A (E120 Backups, v0.1.127), B (E121 `make e2e`) und C (E122 Release-Takt, v0.1.128) erledigt. Offen aus A: Zeitplan läuft im Panel-Prozess (verpasster Lauf wird nicht nachgeholt); keine Vorschau direkt aus dem Bucket; ungeklärt: ESS-Upgrade startete zweimal kurz nacheinander ohne 409.
- **Als Nächstes F: E125 Einladungs-Codes** (MAS-Registrierungstokens), dann G … siehe Programm. Jeder Release über rc (RELEASING.md — OIDC-Umschalten nur mit dem veröffentlichten Chart, e2e über ClusterIPs).
- Offen aus E: `hooks-failed` nicht in der Leiste (bewusst); Was-ist-neu merkt `X.Y.Z-rc.N` als gesehen → nach stable erscheint der Abschnitt auf dem Testcluster noch einmal (Produktion bekommt keine rc); `turn-tls` ohne ETP=Local → BACKLOG P2-35.
- Offen aus D: Hookshot-Zustellung in echten Admin-Raum nicht live bewiesen (Spielwiese ohne Hookshot); Alarm bei `failed`/`hooks-failed` des ESS-Release (S3) als späterer Prüfer; Pod-Schonfrist liegt im Speicher (Neustart verlängert sie um bis zu 10 min).
- Spielwiese: Container `postgres-exporter` in `ess-postgres-0` hat 247 Neustarts (DB selbst 0) — ungeprüft, nicht E123.
- Testcluster: matrixctrl 0.1.130 (per Self-Update), OIDC an, Benachrichtigungen eingerichtet gegen Testempfänger in Namespace `e123` (Webhook-Empfänger + Mailpit), `updateCheck.channel=edge`. Produktion bleibt `stable` (Standard) — bekommt rc nie angeboten. Backup-Ziel der Spielwiese ist der S3-Dienst in Namespace `s3test` (`e120-s3` gelöscht 2026-11-16). `make e2e` braucht OIDC aus (Helm-Wert `oidc.enabled=false`, danach wieder an) und einen leeren `config/diff`.
- Öffentliche Namen der Spielwiesen-Ingresses führen nicht zwingend zur Spielwiese: im Test immer cluster-intern prüfen (port-forward), nie über den öffentlichen Namen.
- Bekannt aus E121: Die Fehlermeldung bei falschem Passwort im Login-Formular zeigt nicht die Servermeldung.
Noch nicht in Produktion bewiesen: Port-Blockade-Auflösung (119c), MAS-Sitzungs-Aufräumen (119d), Föderations-Gegenstellen.

## Entscheidungen (mit Kurzbegründung)
- Release-Kanäle (Betreiber 2026-11-17): rc-Tags statt Builds pro Push; stable wird vom selben Commit neu gebaut statt umetikettiert (Binary nennt sonst die rc-Version); Kanal nur per Helm-Wert (§4.131).
- Hooks abgelöst statt für alle eingeschaltet — ESS setzt hostNetwork/ETP selbst (§4.125).
- Git-Historie nicht umgeschrieben (ID-Präfix in v0.1.123-Test): würde zwei Release-Tags versetzen und Versionen neu veröffentlichen; GitHub behält den Commit ohnehin.
- Worker-Urteil am 95. Perzentil einer Woche, nicht am Schnitt (§4.123).
- MatrixCtrl betreibt fremde Bridges nicht, es meldet sie nur an/zeigt sie (§4.128).
- e2e ändert „Name der App" (Betreiber 2026-11-14): nur Element Web startet neu. Der Lauf verweigert, wenn fremde Änderungen anstehen — nie blind verwerfen: auf der Spielwiese war darunter eine nötige CPU-Absenkung.

## Relevante Dateien
docs/ROADMAP.md (Etappen-Tabelle) · docs/DESIGN.md §4.119–4.128 · CHANGELOG.md · docs/plans/etappe-1*.md ·
internal/{federation,synmetrics,dnshealth,bridges,ociregistry}/ · internal/mas/sessions.go · internal/k8s/hostport.go ·
web/scripts/{e2e.mjs,verify-ui.mjs,lib/}
