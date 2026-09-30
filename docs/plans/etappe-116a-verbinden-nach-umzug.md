# Etappe 116a — Matrix-Login nach einem Umzug verbinden

Stand 2026-09-30, Betreiber-Meldung vom neuen Server:

> Benutzerverwaltung braucht MAS-Zugang … läuft im Bootstrap-Modus
> zudem wenn man auf Räume oder Moderation klickt kommt der Verbinden-Button, der aber
> nichts macht — Ladeanzeige, aber es öffnet sich kein Tab

## Befund (gelesen, nicht vermutet)

1. **Der Zustand:** Auf dem neuen Server hat MatrixCtrl keine OIDC-Einstellungen (weder Env
   noch Datenbank) — nach dem Umzug per `recover-login` hereingekommen. Die
   MAS-Registrierung `additional.0-matrixctrl-client` ist mit der Konfiguration umgezogen,
   MAS kennt den Client. Die Rücksprung-Adresse darin nennt aber den Hostnamen des alten
   Panels; das neue läuft unter einem anderen.
2. **Der tote Knopf:** `MatrixConnect` ruft `/api/v1/rooms/connect`, der Server antwortet
   `501 Matrix-Login ist nicht konfiguriert`, und die Komponente zeigt den Fehler nie an —
   der Knopf springt zurück, sonst nichts.
3. **Der Weg, der helfen soll, hilft nicht:** Setup → „Matrix-Login verbinden" findet die
   Registrierung, fragt MAS, MAS kennt den Client → `reconcileMASClient` → „bereits
   vollständig registriert". Weder wird die neue Rücksprung-Adresse eingetragen noch werden
   die Zugangsdaten in MatrixCtrl gespeichert. Der Betreiber läse „erfolgreich" und bliebe
   im Bootstrap-Modus.

## Was gebaut wird

1. **Setup-Verbinden kennt den Umzug:** Registrierung vorhanden und MAS kennt den Client,
   aber MatrixCtrl hat keine gespeicherten Einstellungen **oder** die Rücksprung-Adresse des
   aktuellen Panels fehlt → die Adresse wird ergänzt (die alte bleibt), dann der vorhandene
   Weg `connectUpgrade`: ESS deployen, MAS bestätigen lassen, erst dann umschalten (§4.88).
   Nur wenn beides stimmt, bleibt es beim „bereits registriert".
2. **Kein toter Knopf:** Räume, Moderation und Benutzer fragen, ob der Matrix-Login
   eingerichtet ist; wenn nicht, erklären sie es in einem Satz und führen zum Setup, statt
   einen Knopf zu zeigen, der nicht funktionieren kann. `MatrixConnect` zeigt jeden Fehler
   an.

## Fertig wenn

- Test: Registrierung mit alter Adresse, keine gespeicherten Einstellungen → die neue
  Adresse wird ergänzt, die alte bleibt, Client-ID und Secret unverändert.
- Test (Gegenprobe): alles vorhanden → nichts geändert.
- Der Verbinden-Knopf zeigt einen Serverfehler an.
