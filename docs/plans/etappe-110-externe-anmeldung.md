# Etappe 110 — Anmeldung mit Google, GitHub und eigenem OIDC-Anbieter

Stand 2026-09-29. Vorgezogen aus „Aufgaben I" (der Plan 107 nannte den Login-Assistenten
als erste Aufgabe); Aufgaben I/II und Expertenebene werden 111–113.

## Entscheidungen des Betreibers (2026-09-29)

1. **Einmal, zentral in MAS.** Element und MatrixCtrl teilen sich den Login; MatrixCtrl
   lässt weiter nur MAS-Admins herein (§4.5). Kein zweiter Anbieter für MatrixCtrl.
2. **Jeder darf sich über die externen Anbieter registrieren.** Offene Registrierung —
   der Server ist für jeden mit einem Google-/GitHub-Konto offen.
3. Anbieter: Google, GitHub, generischer OIDC (Entscheidung aus Plan 107).

## Was nachgelesen wurde (MAS 1.25.1, Chart 26.9.3)

- Das Chart belegt nur `http`, `database`, `telemetry`, `matrix`, `secrets`.
  `upstream_oauth2`, `account`, `captcha` sind frei.
- `matrixAuthenticationService.additional.<key>.configSecret/configSecretKey` hängt
  MAS-Konfiguration aus einem Kubernetes-Secret ein. **Das Client-Secret landet nie im
  Git-Repo der Einstellungen.**
- Callback je Anbieter: `https://<MAS-Host>/upstream/callback/<ULID>`. Die ULID muss
  **vor** dem Anlegen der App beim Anbieter feststehen und darf sich danach nie ändern.
- `upstream_oauth2`-Einträge werden beim Start in die DB synchronisiert; **entfernte**
  Einträge bleiben, bis `config sync --prune` läuft. Abschalten heißt `enabled: false`.
- Übernahme-Risiko: `localpart.on_conflict` ≠ `fail` verknüpft automatisch mit
  bestehenden Accounts gleichen Namens (MAS-Doku: „account takeover"). Die
  MAS-Beispielkonfigurationen setzen `localpart: ignore` — der Nutzer wählt seinen Namen.
- Bestehende Accounts verknüpfen sich sicher über die MAS-Kontoseite („account linking
  from the account UI").

## Sichere Voreinstellungen (nicht abwählbar im Assistenten)

| Einstellung | Wert | Warum |
|---|---|---|
| `claims_imports.localpart.action` | `ignore` | Kein Name vom Anbieter → keine Kollision, kein Übernahmepfad |
| `claims_imports.localpart.on_conflict` | `fail` (Default, nicht gesetzt) | dito |
| `claims_imports.email.action` | `suggest` | Adresse wird angeboten, nicht erzwungen, nicht zum Verknüpfen benutzt |
| `registration_token_required` | `false` | Entscheidung 2 |
| `pkce_method` | Default `auto` | |
| Secret | Kubernetes-Secret `matrixctrl-mas-upstream` in `ess` | nie im Config-Repo, nie im Log, nie in einer API-Antwort |
| Entfernen | `enabled: false` | siehe oben |
| Passwort-Login | bleibt an | Notzugang und bestehende Accounts |

Zu Entscheidung 2 gehört ein sichtbarer Satz im Assistenten, was „offen" heißt, und der
Hinweis auf Admin-Rechte: wer sich registriert, ist Nutzer, nie Admin.

## Was gebaut wird

1. **`internal/masupstream`** — Modell (Art, ULID, Anzeigename, Issuer, Client-ID,
   Client-Secret, aktiv), ULID-Erzeugung, Rendern der `upstream_oauth2.providers` mit den
   Voreinstellungen oben je Art (Google/GitHub nach den MAS-Beispielen, OIDC mit
   Discovery). Reine Funktionen, getestet.
2. **Speicher:** das Secret hält `upstream.yaml` (was MAS liest) und `providers.json`
   (was der Assistent liest; Entwürfe ohne Zugangsdaten werden gespeichert, aber nicht
   gerendert — MAS verweigert den Start mit leerer Client-ID).
3. **Einhängen:** fehlt in der MAS-Sektion `additional.matrixctrl-upstream`, wird er
   über den kommentarerhaltenden Weg (`SetSectionValues`) eingetragen → erscheint als
   ausstehende Änderung, Übernehmen startet MAS neu. Ist er schon deployt, ändert sich
   beim Speichern nur das Secret → MatrixCtrl startet MAS gezielt neu (`RolloutRestart`),
   weil sich das Pod-Template nicht ändert.
4. **API** (`/api/v1/login-providers`): Liste (ohne Secrets, mit Callback-URL), Anlegen
   (erzeugt ULID, Entwurf), Zugangsdaten setzen, OIDC-Discovery testen, Abschalten.
5. **Seite „Anmeldung"** unter Konfiguration: Karten je Anbieter; Assistent in drei
   Schritten — Art wählen → Callback-URL kopieren + Schritte beim Anbieter → Client-ID/
   -Secret eintragen, prüfen, speichern.
6. **Nachweis nach dem Neustart:** die Login-Seite von MAS wird über den internen Dienst
   gelesen (nicht über die öffentliche Adresse — Pods erreichten Cloudflare am 28.09.
   nicht) und muss den Anzeigenamen des Anbieters enthalten.

## Randfälle

- MAS-Host unbekannt (keine Ingress-Angabe in der Config) → Callback-URL nicht erfindbar;
  der Assistent sagt das und bricht vor dem Anlegen ab.
- Secret existiert, aber `additional`-Eintrag fehlt (von Hand gelöscht) → wieder
  eintragen, als ausstehende Änderung.
- OIDC-Discovery nicht erreichbar → Speichern trotzdem erlaubt, aber als „nicht geprüft"
  gesagt (§4.55).
- Beide Auth-Modi von MatrixCtrl: alles nur für Admins.

## Fertig wenn

- Ein Google-Anbieter lässt sich anlegen, ohne dass das Client-Secret im Config-Repo, in
  einer API-Antwort oder im Log steht (Test durchsucht alle drei).
- Die gerenderte MAS-Konfiguration setzt `localpart: ignore` für jede Art (Test).
- Nach Übernehmen zeigt die MAS-Login-Seite „Mit Google anmelden" (Live, auf dem neuen
  Server — der Betreiber legt die App bei Google an).
- Abschalten setzt `enabled: false` und entfernt nichts.
