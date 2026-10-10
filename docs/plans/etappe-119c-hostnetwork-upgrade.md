# Etappe 119c — Upgrades, die am Anruf-Server hängen bleiben

Stand 2026-10-10, Betreiber-Screenshots: Upgrade auf ESS 26.10.0, dreimal nach 10 Minuten
`helm upgrade: context deadline exceeded`; Meldung „Helm hat die vorherige Revision
automatisch wiederhergestellt"; Hinweis „Post-Upgrade-Hooks laufen automatisch"; unten
links eine ULID statt des Namens.

## Befund (gelesen)

- `helm history`: Revisionen 14–16 `failed`, 13 (26.9.4) `deployed`. Die Pods laufen
  größtenteils schon in der neuen Revision — es wurde **nichts** wiederhergestellt; ESS
  upgradet ohne `--atomic`. Die Meldung behauptet das Gegenteil.
- Neuer SFU-Pod seit 6 h `Pending`: „didn't have free ports for the requested pod ports".
  Der SFU läuft mit `hostNetwork`, eine Replik, `maxUnavailable: 0`: Der neue Pod braucht
  die Ports, die der alte hält, der alte geht erst, wenn der neue bereit ist. Bekannt
  (P2-23) — „Anruf-Server neu starten" umgeht es per Pod-Löschung —, aber nicht im
  Upgrade. Früher unsichtbar, weil `hostNetwork` erst nach dem Upgrade per Hook kam; seit
  es in den Werten steht (und mit 0.1.122 für jede Neuinstallation), trifft es jede
  Änderung am SFU-Template.
- Der Hook-Hinweis auf der Upgrade-Seite ist seit 0.1.122 falsch.
- Die Sitzung trägt die MAS-`sub` (ULID), weil MAS keine Matrix-ID im Userinfo liefert.

## Was gebaut wird

1. **Blockade lösen, während Helm wartet:** Ein `Pending`-Pod mit `hostNetwork`, den der
   Scheduler wegen belegter Ports ablehnt, und ein laufender Pod derselben Deployment aus
   einem anderen ReplicaSet → der alte wird gelöscht, wie bei „Anruf-Server neu
   starten". Gesagt im Log, mit dem Preis (laufende Anrufe brechen kurz ab). Nur in
   genau diesem Muster; Entscheidung als reine Funktion mit Tests.
2. **Die Fehlermeldung sagt, was ist:** kein Zurückrollen erfunden; Zustand `failed`, Teile
   laufen neu, erneut versuchen oder zurückrollen.
3. **Hook-Hinweis aus der Abdeckung:** nur Hooks, die wirklich etwas tun werden.
4. **Name statt ULID:** `/auth/me` löst die ULID über die MAS-Admin-API in den
   Benutzernamen auf.

## Fertig wenn

- Tests: genau das Muster wird erkannt; kein Opfer ohne Port-Ablehnung, ohne
  `hostNetwork`, aus derselben ReplicaSet oder einer fremden Deployment — je mit
  Gegenprobe.
- Produktion: Upgrade auf 26.10.0 läuft nach dem MatrixCtrl-Update durch.
