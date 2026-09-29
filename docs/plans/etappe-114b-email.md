# Etappe 114b — E-Mail-Versand

Stand 2026-09-29. Ohne E-Mail laufen zwei Dinge aus 113/114 ins Leere: „E-Mail-Adresse bei
der Registrierung verlangen" und das Zurücksetzen vergessener Passwörter.

## Nachgelesen (MAS 1.25.1)

```yaml
email:
  from: '"Name" <auth@example.org>'
  transport: smtp        # oder blackhole (Standard: nichts senden)
  mode: plain | tls | starttls
  hostname: …
  port: 587
  username: …
  password: …
```

`password` steht im Klartext in der Konfiguration — also wie die Client-Secrets der
Anbieter (§4.112) **nicht** ins Git-Repo: Secret `matrixctrl-mas-email` mit `email.yaml`,
eingehängt über `additional.matrixctrl-email.configSecret`. Da das Passwort mit den
übrigen Feldern ein Dokument bildet, liegt der ganze `email:`-Block im Secret; der
Assistent liest ihn von dort und gibt das Passwort nie zurück.

## Was gebaut wird

1. **`internal/mail`** — Modell (Absender, Antwort-an, Host, Port, Verschlüsselung,
   Benutzer, Passwort), Rendern des `email:`-Blocks, und **`Probe`**: Verbindung, `EHLO`,
   `STARTTLS`/TLS, `AUTH`, `QUIT` — ohne Mail. Dazu `Send` für eine echte Testmail an
   eine eingegebene Adresse, weil nur die beweist, dass es beim Empfänger ankommt.
2. **API** `/api/v1/mail`: lesen (ohne Passwort), speichern (Secret + Einhängen wie
   §4.112: fehlt die Referenz → ausstehende Änderung, sonst MAS neu starten), prüfen,
   Testmail senden, abschalten (`transport: blackhole`).
3. **Karte „E-Mail" in den Aufgaben** — verlinkt auf eine eigene Seite, weil Passwort,
   Prüfung und Testmail nicht in eine Zeile passen.
4. Vorauswahl nach Port: 465 → TLS, 587 → STARTTLS, 25 → keine. Änderbar.

## Randfälle

- Kein Absender → MAS startet mit `transport: smtp` nicht; Pflichtfeld, vorbelegt mit
  `noreply@<servername>`.
- Prüfung scheitert → gespeichert wird trotzdem, aber als „nicht geprüft" gesagt (§4.55).
- Selbstsigniertes Zertifikat am Mailserver: wird gemeldet, nicht stillschweigend
  akzeptiert.
- Timeout 10 s; ein hängender Mailserver darf die Seite nicht blockieren.

## Fertig wenn

- Das Passwort steht im Secret und in keiner API-Antwort und in keiner Datei des Repos
  (Test durchsucht beides).
- Die Prüfung gegen einen Server, der AUTH ablehnt, sagt das in einem Satz (Test mit
  eigenem SMTP-Server im Test).
- Abschalten schreibt `transport: blackhole` und lässt die übrigen Werte stehen.
