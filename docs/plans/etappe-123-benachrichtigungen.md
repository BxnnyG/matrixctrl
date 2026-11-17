# Etappe 123 — Benachrichtigungen: Webhook in den Admin-Raum und E-Mail

Stand 2026-11-17. Paket D aus `programm-2026-11.md`. Heute merkt der Betreiber einen
DNS-Ausfall, ein auslaufendes Zertifikat, einen hängenden Pod oder ein gescheitertes
Backup nur, wenn er ins Panel schaut. Ziel: MatrixCtrl meldet sich selbst — einmal beim
Auftreten, auf Wunsch als Erinnerung, und mit Entwarnung, wenn es vorbei ist.

## Befund (gelesen)

- Keine Benachrichtigungs-Schicht. DESIGN S3/S4 nennen „no alerting" als Lücke; §4.129 sagt,
  Backup-Fehlschläge kommen „mit Etappe Benachrichtigungen (über offsite_runs)".
- Signale: `dnshealth.State()` (Ausfall = `OK==false`, nur Timeout/SERVFAIL), Ticker 1 min ·
  `updatecheck.Checker` (`Result.Available/Latest`, Kanal) · `k8s` `ComponentHealth`
  (`Looping`, `Ready`, `Unschedulable`; Deployments **und** StatefulSets), nur auf Anfrage ·
  `offsite_runs` (`status failed`, `error`) · Zertifikate: `tlscheck` prüft nur auf Anfrage
  über das Netz; Ingress-TLS-Secret-Namen kennt `k8s/secrets.go`.
- Versand: `internal/mail` (`Send`, `Probe`) mit SMTP aus der Mail-Seite (Secret
  `matrixctrl-mas-email`). Kein Matrix-Sendeweg, kein Bot.
- Persistenz: Postgres, Migrationen `internal/db/migrations/NNN_*.sql` (neueste 019),
  Einstellungen als JSON-Zeile in `instance_settings`, Geheimnisse in K8s-Secrets.

## Betreiber-Entscheidungen (2026-11-17)

1. Matrix-Weg = **eingehende Webhook-URL** (z. B. Hookshot → Admin-Raum; geht auch mit
   Slack/ntfy). Kein Bot-Konto.
2. E-Mail standardmäßig über den **MAS-SMTP-Zugang**, in MatrixCtrl auf einen **eigenen
   SMTP-Zugang** umstellbar.
3. Erinnerung bei fortbestehendem Problem **einstellbar**: aus / 6 h / 24 h.

## Was gebaut wird

1. `internal/notify` (Logik, zuerst getestet):
   - `Alert{Key, Trigger, Title, Detail}`; Prüfer liefern die *aktuell* bestehenden Alerts.
   - Zustandsmaschine pro Key: neu → „Problem"-Nachricht · besteht weiter und Abstand
     erreicht → „Erinnerung" · verschwunden → „Entwarnung". Zustand in Tabelle
     `notify_state` (Migration 020) — ein Neustart verschickt nichts doppelt.
   - Versand gescheitert → Zustand **nicht** als gesendet markieren, nächster Lauf
     versucht erneut; letzter Fehler je Kanal wird gehalten und angezeigt.
   - Prüfer: **DNS** (aus `dnshealth`) · **Zertifikat** < 14 Tage (TLS-Secrets der Ingresses
     im ESS-Namespace über client-go lesen und x509 parsen — kein Netz, keine öffentlichen
     Namen) · **Update** (einmal je neuer `Latest`; Entwarnung entfällt, „erledigt" wenn
     installiert) · **hängender Pod** (`Looping` oder nicht bereit/unplanbar > 10 min;
     `scaled-zero` zählt nicht) · **Backup** (jüngster abgeschlossener `offsite_runs`-Lauf
     `failed`; nächster `ok` = Entwarnung).
2. Kanäle: **Webhook** (POST JSON `{"text", "html"}`, Timeout 10 s; URL ist ein Geheimnis →
   K8s-Secret `matrixctrl-notify`) · **E-Mail** über `internal/mail` an eingetragene
   Empfänger; SMTP-Quelle `mas` (Standard) oder `eigen` (eigene Felder, Passwort im selben
   Secret). Ohne SMTP ist E-Mail ausgegraut.
3. Einstellungen als `instance_settings`-Zeile `notifications`: Kanäle an/aus, Empfänger,
   SMTP-Quelle, Erinnerungsabstand, je Auslöser an/aus.
4. Ticker in `main.go` (1 min; Zertifikate stündlich), jeder k8s-Aufruf mit Timeout.
5. API (nur Admin, beide Anmeldearten): `GET/PUT /api/v1/notifications/settings`,
   `POST /api/v1/notifications/test` (Kanal wählbar, meldet Fehler im Wortlaut),
   `GET /api/v1/notifications/state` (aktive Probleme, letzter Versand, letzter Fehler).
   Änderungen und Testversand landen im Audit.
6. Oberfläche: Seite „Benachrichtigungen" (Route `notifications.tsx`, Menü), mc.tsx-Primitives,
   Testnachricht-Knopf je Kanal, Liste aktiver Probleme. `make e2e` kennt die Route automatisch.

## Gegenproben (müssen fallen)

- Entwarnung entfernt → Test „Problem weg ⇒ Entwarnung" rot.
- Fehlschlag als gesendet markiert → Test „Webhook 500 ⇒ nächster Lauf sendet erneut" rot.
- Zustand nur im Speicher → Test „Neustart sendet nicht doppelt" rot.
- Zertifikatsschwelle verschoben (14 → 1) → Test „13 Tage ⇒ Alert" rot.
- StatefulSet ausgelassen → Test „ess-postgres hängt ⇒ Alert" rot.

## Grenzfälle (PROZESS-Liste)

1 Kein ESS: Pod-/Zertifikat-Prüfer liefern nichts, kein Fehlalarm. 2 Release `failed`:
nicht Teil dieser Etappe (siehe unten). 3 StatefulSets/Multi-Container: über `ComponentHealth`.
4 Cluster langsam: Timeout → Prüfer meldet „unbekannt", **kein** Alarm und **keine**
Entwarnung. 5 Kein Internet: Webhook scheitert sichtbar, Wiederholung im nächsten Lauf;
Update-Prüfer ohne Ergebnis schweigt. 6 Beide Anmeldearten: Routen wie die übrigen
Einstellungen. 7 Keine Einstellung gespeichert → alles aus, Seite erklärt das.

## Nicht in dieser Etappe

Alarm bei `failed`/`hooks-failed` des ESS-Releases (S3) — Prüfer-Schnittstelle lässt ihn als
späteren Einzeiler zu; Bot-Konto; Zustellung in verschlüsselte Räume ohne Hookshot;
Warnleiste im Panel (Paket E).

## Fertig wenn

`make check` grün; Gegenproben fallen; auf dem Testcluster per rc: Testnachricht kommt im
Admin-Raum (Hookshot) und per E-Mail an; DNS-Ausfall (CoreDNS kurz auf 0) und ein
gescheiterter Backup-Lauf (falscher S3-Zugang) lösen je Nachricht + Entwarnung aus;
Zertifikat- und Pod-Prüfer durch Tests belegt, Pod zusätzlich live (Element Web
auf der Spielwiese mit kaputtem Image, danach zurück). `make e2e` + vier S11-Checks, dann stable-Tag; ROADMAP/DESIGN nachgezogen.
