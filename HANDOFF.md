# HANDOFF

## Ziel
MatrixCtrl: Day-2-Admin-Schicht für ESS. Alle Roadmap-Seiten (Phase 1–5) sind gebaut; jetzt
Lücken schließen und Betreiber-Bedürfnisse (siehe „Offen").

## Erledigt (Stand 0.1.126, 2026-10-12)
- 116–116d: Update aus dem Panel (Job, atomic), Update-Karte + „Jetzt prüfen", Matrix-Login nach Umzug, Notzugang-Hinweise.
- 117 Föderation · 118 Worker-Insights · 119 Bridges (Hookshot + Appservices).
- 119a DNS-Überwachung · 119b Hooks überspringen, wenn das Chart sie abdeckt · 119c SFU-Port-Blockade beim Upgrade · 119d MAS-Sitzungen aufräumen.
- 0.1.126: Hookshot-Anleitung nach erstem echten Durchlauf (Gruppenraum, Moderator, Adresse im Direktchat). Webhook Ende-zu-Ende auf dem Testcluster bestätigt.
- Orchestrator-Modus eingerichtet (ORCHESTRATOR.md, .claude/agents/).

## Offen / Nächster Schritt
Betreiber wählt; Empfehlung in dieser Reihenfolge:
1. Automatische Backups mit Zeitplan + Ziel außerhalb des Servers (S3-kompatibel); Backup vor jedem ESS-Upgrade. Heute nur manuell.
2. Benachrichtigungen bei Problemen (DNS, Zertifikat, Update, hängender Pod, Föderation) — per Hookshot-Webhook in einen Admin-Raum oder E-Mail.
3. Einladungs-Codes (MAS-Registrierungstokens).
Weiter: Bridge aus dem Panel anmelden; Schalter `externalTrafficPolicy` für die Anruf-Ports (Aufgaben-Feld mit mehreren Pfaden); Server-Mitteilung an alle; Speicherplatz/Aufräumen; englische UI (P3-2); Restore-Probe gegen live ESS (§4.106); Audit-Aufbewahrung (Betreiber-Entscheidung, P2-19).
Noch nicht in Produktion bewiesen: Port-Blockade-Auflösung (119c), MAS-Sitzungs-Aufräumen (119d), Föderations-Gegenstellen.

## Entscheidungen (mit Kurzbegründung)
- Hooks abgelöst statt für alle eingeschaltet — ESS setzt hostNetwork/ETP selbst (§4.125).
- Git-Historie nicht umgeschrieben (ID-Präfix in v0.1.123-Test): würde zwei Release-Tags versetzen und Versionen neu veröffentlichen; GitHub behält den Commit ohnehin.
- Worker-Urteil am 95. Perzentil einer Woche, nicht am Schnitt (§4.123).
- MatrixCtrl betreibt fremde Bridges nicht, es meldet sie nur an/zeigt sie (§4.128).

## Relevante Dateien
docs/ROADMAP.md (Etappen-Tabelle) · docs/DESIGN.md §4.119–4.128 · CHANGELOG.md · docs/plans/etappe-1*.md ·
internal/{federation,synmetrics,dnshealth,bridges,ociregistry}/ · internal/mas/sessions.go · internal/k8s/hostport.go
