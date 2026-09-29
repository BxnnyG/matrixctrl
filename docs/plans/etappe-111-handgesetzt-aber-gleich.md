# Etappe 111 — Von Hand gesetzt, aber gleich dem Chart

Stand 2026-09-29. Anlass: Die Startseite warnt laut „2 Felder von Hand gesetzt, ohne dass
ein Hook es trägt" für die Ressourcen von Postgres und Synapse. Die Werte wurden am 28.09.
per `kubectl set resources` gesetzt und danach **mit denselben Werten** in die Einstellungen
übernommen. Helm will also genau das, was dort steht; weil der Wert schon stimmte, hat
Helms Drei-Wege-Merge das Feld nicht neu geschrieben, und `kubectl` bleibt als Besitzer
eingetragen. Der Bericht liest die Besitzer richtig — die Schlussfolgerung „überlebt jedes
Upgrade unbemerkt" ist in diesem Fall falsch: es gibt nichts, was überleben müsste.

Zweitens zählt die Meldung Objekte, nennt sie aber „Felder": 2 Objekte, 7 Felder.

## Was gebaut wird

1. `drift.ManualEdit.MatchesChart`: wahr, wenn **jedes** der Felder live denselben Wert hat
   wie im Manifest des laufenden Releases. Mengenangaben werden als Kubernetes-Quantity
   verglichen (`1` = `1000m`, `1536Mi` = `1.5Gi`), sonst exakt.
2. Solche Einträge zählen nicht mehr als „ohne Hook, überlebt unbemerkt" (laut), sondern
   als eigene leise Zeile: von Hand gesetzt, steht aber inzwischen auch so im Chart.
3. Wortlaut: „von Hand gesetzte Felder in N Objekten".
4. Nicht lesbares Manifest → kein Urteil, Einträge bleiben laut (unbekannt ≠ harmlos).

## Fertig wenn

- Test: Live `cpu: "1"`, Manifest `cpu: 1000m` → gleich; Live 1Gi, Manifest 4Gi → laut.
- Live auf dem Cluster: die Postgres-/Synapse-Einträge landen in der leisen Zeile.
