# Etappe 116d — Notzugang auf Räumen und Moderation

Stand 2026-09-30, Betreiber-Meldung nach 0.1.117: „Benutzer geht, Räume und Moderation
nicht."

## Befund (Log des neuen Servers)

`/api/v1/auth/me` → `admin`: Der Tab war noch mit dem Notzugang angemeldet. Benutzer geht,
weil es mit MatrixCtrls eigenem MAS-Client arbeitet. Räume und Moderation handeln mit dem
Matrix-Konto des Betreibers: „Verbinden" lief erfolgreich durch MAS (Callback 302 ohne
Fehler), das Token wurde unter dem Matrix-Konto abgelegt — und die Notzugang-Sitzung fragt
unter `admin`. Zurück auf der Seite, wieder „Verbinden", im Kreis.

## Was gebaut wird

- `/rooms/state` sagt einer Notzugang-Sitzung `session: "bootstrap"` und warum;
  `/rooms/connect` lehnt für sie mit 409 und demselben Satz ab, statt eine Anmeldung zu
  starten, die ihr nicht helfen kann.
- `MatrixConnect` (Räume und Moderation) zeigt dann „Du bist mit dem Notzugang
  angemeldet" mit „Abmelden und über Matrix anmelden" und startet nichts von selbst.

## Fertig wenn

- Go-Test: Status und Verbinden für `admin`; eine Matrix-Sitzung unverändert
  (Gegenprobe: ohne die Abfrage fällt er).
- Frontend-Test: Notzugang → Hinweis und kein Aufruf von `/rooms/connect`
  (Gegenprobe ebenso).
