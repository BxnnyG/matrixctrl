# Etappe 116c — Das Update aus dem Panel muss man finden können

Stand 2026-09-30, Betreiber-Frage nach 0.1.116:

> ok aber wo ist der Upgrade-Button für MatrixCtrl und der Check dafür, oder ein Textfeld,
> das sagt: du kannst hier mit einem Klick upgraden

## Befund

- Etappe 116 hat das Update aus dem Panel gebaut und es hinter ein 10-px-Kästchen unten
  links gelegt, das **nur erscheint, wenn schon ein Update bekannt ist**. Wer es nicht
  sieht, erfährt nirgends, dass es die Funktion gibt.
- Bekannt ist ein Update frühestens nach **6 Stunden**: So lange merkt sich die Prüfung ihr
  Ergebnis. Der neue Server ist um 07:50 mit 0.1.115 gestartet, 0.1.116 kam um 10:10 —
  das Kästchen wäre gegen 13:50 erschienen. Einen „Jetzt prüfen"-Knopf gibt es nicht.
- Die Seite „Updates" (`/helm`) handelt nur von ESS.

## Was gebaut wird

1. **Prüfen auf Knopfdruck:** `GET /api/v1/version?refresh=1` fragt die Registry sofort,
   höchstens einmal pro 30 s (danach die gemerkte Antwort) — ein Knopf darf GHCR nicht
   hämmern. Ohne Knopf: 1 h statt 6 h.
2. **Eine MatrixCtrl-Karte oben auf „Updates"**, immer da, nicht nur bei einem Update:
   installierte und neueste Version, wann geprüft, „Jetzt prüfen". Dazu genau ein Satz,
   was hier geht:
   - Update da, Rechte da → „Auf X aktualisieren" (der vorhandene Dialog) und was dabei
     passiert; Link auf die Release-Notes.
   - Update da, Rechte fehlen → der Befehl, einmalig.
   - Aktuell → „Kommt eine neue Version, installierst du sie hier mit einem Klick."
   - Läuft gerade ein Update → sagt das.
   - Nur-Lesen-Konto → kein Knopf.
3. Die Versionsnummer unten links führt auf diese Seite.

## Fertig wenn

- Test: `Refresh` holt trotz frischem Cache neu (Gegenprobe: mit `Check` statt Fetch
  bleibt die alte Antwort); zweimal innerhalb von 30 s → ein Abruf.
- Test: Die Karte zeigt den Knopf bei Update + Rechten, den Befehl bei fehlenden Rechten,
  den Ein-Klick-Satz wenn aktuell.
