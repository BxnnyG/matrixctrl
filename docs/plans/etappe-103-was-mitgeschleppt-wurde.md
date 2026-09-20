# Etappe 103 — Was mitgeschleppt wurde

> „ich kann hier aber immernoch nicht upgraden kannste das fixen!"

Der Operator hat `install.sh update` ausgeführt. Es lief durch, ohne einen Fehler:

```
Version
  installed  0.1.88
  published  0.1.90
Upgrading to 0.1.90
Release "matrixctrl" has been upgraded. Happy Helming!
REVISION: 78
Now running
ghcr.io/bxnnyg/matrixctrl:0.1.70
✓ done
```

Zwei Zeilen untereinander: `Upgrading to 0.1.90` und `0.1.70`. Dazwischen ein Haken.

## Was gemessen wurde

Nicht geraten, sondern nachgesehen:

```
$ helm get values matrixctrl -n matrixctrl
USER-SUPPLIED VALUES:
image:
  tag: 0.1.70
```

```
$ helm show values oci://ghcr.io/bxnnyg/charts/matrixctrl --version 0.1.90
image:
  tag: "0.1.90"      # korrekt — CI pinnt beim Release
```

Die Kette war an jeder Stelle heil: Chart 0.1.90 veröffentlicht, Image 0.1.90
veröffentlicht, CI pinnt den Tag auf die eigene Version, die Registry liefert beides
aus. Kaputt war nur die letzte Übergabe — ein vom Operator gelieferter Wert schlägt
die Chart-Voreinstellung, und zwar für immer.

`helm history` datiert den Ursprung auf Revision 73, den Upgrade auf 0.1.70 am
5. September. Dort wurde einmal ein `image.tag` gesetzt. Seitdem hat **jedes** Upgrade
ihn weitergetragen: Revision 74 auf Chart 0.1.78, Revision 76 auf 0.1.88,
Revision 78 auf 0.1.90 — fünf Chart-Upgrades lang lief unverändert das Image vom
5. September. Der Operator hat das über Wochen nicht sehen können, weil die
Oberfläche die Chart-Version anzeigt und die stimmte ja.

## Die Ursache, und warum sie bitter ist

`cmd_update` liest die bisherigen Werte mit `helm get values` und reicht sie mit `-f`
wieder ein. Darüber steht dieser Kommentar (Zeile 477):

> Values the operator supplied before are carried over explicitly rather than with
> `--reuse-values`, which also freezes the *chart's* defaults at their old version —
> the way an upgrade silently keeps shipping last release's settings.

Die Begründung ist richtig. Genau das ist dann trotzdem passiert, mit demselben
Ergebnis und aus demselben Grund, nur über einen anderen Weg: Einmal gesetzt, ist
`image.tag` kein Einmal-Wert mehr, sondern Teil der Release-Werte, und die werden
bei jedem Upgrade brav wieder eingereicht. Der Mechanismus, der das Einfrieren
verhindern sollte, friert den einen Wert ein, auf den es ankommt.

Der Tag ist nämlich **keine Instanzeinstellung**. DESIGN §4.17 und der Kopf des
CHANGELOG sagen dasselbe: *Chart-Version == appVersion == Image-Tag, ein Artefakt,
eine Nummer.* Was dem Chart gehört, darf nicht aus alten Release-Werten kommen.

## Die zweite Ursache, die schlimmere

`cmd_update` liest das laufende Image nach dem Upgrade aus — und vergleicht es nicht.

```bash
head_ "Now running"
kubectl ... -o jsonpath='{...containers[0].image}'   # gedruckt
ok "done"                                            # und Haken
```

Der richtige Wert wurde geholt, benannt, angezeigt — und nicht verdrahtet. Das ist
in diesem Repository der siebte Fall derselben Sorte (§4.79, §4.84, §4.90, §4.92,
§4.95, §4.99). Ohne diesen zweiten Fehler wäre der erste eine Meldung gewesen statt
fünf Wochen falsches Image: das Skript *hatte* die Zahl, die den Widerspruch beweist,
und hat einen Haken darunter gesetzt.

Merksatz für die Sammlung: **eine Messung, die nur gedruckt wird, ist keine Prüfung.**

## Vier Kopien, nicht eine

Der Block aus acht Zeilen (`mktemp`, `helm get values`, null-Prüfung, `-f`, `rm`)
steht viermal im Skript:

| Zeile | Kommando | betroffen |
|---|---|---|
| 480 | `install` über eine bestehende Installation | ja |
| 650 | `doctor` (Probelauf) | ja — der Probelauf prüft das falsche Image |
| 847 | `update` | ja, der gemeldete Fall |
| 960 | `recover-login` | ja |

Regel 3 (Zentralisierung) gilt: eine Funktion, nicht vier Kopien. Sonst wird dieser
Fix an einer der vier Stellen in drei Monaten wieder fehlen.

## Was gebaut wird

1. **`carry_values()`** — eine Funktion, die die bisherigen Werte in eine Datei legt.
   Alle vier Aufrufstellen benutzen sie. Nichts wird dupliziert.
2. **Der Tag kommt vom Chart, nicht aus der Vergangenheit.** `update` und `install`
   setzen `--set image.tag=<Zielversion>` *nach* dem `-f`, wo es die alten Werte
   schlägt. Das ist deterministisch (kein YAML-Chirurgie-Versuch an fremdem Text)
   und selbstheilend: der falsche Pin wird beim nächsten Lauf überschrieben statt
   nur umgangen, `helm get values` erzählt danach die Wahrheit.
   Bei `doctor`/`recover-login` wird kein Tag gesetzt — die ändern die Version nicht.
3. **Der Abgleich am Ende.** Läuft nach dem Upgrade nicht das Ziel-Image, ist das
   kein Haken, sondern ein Abbruch mit Exit-Code, der Ursache und Reparaturbefehl
   nennt. Aus der gedruckten Zahl wird eine Bedingung.
4. **`status` zeigt beide Nummern.** Chart-Version und Image-Tag getrennt, damit der
   Auseinanderlauf sichtbar ist, ohne dass man ihn vermutet. Heute zeigt die
   Oberfläche nur die eine — deshalb konnte das fünf Wochen lang unbemerkt bleiben.

## Bewusst nicht

- **Kein Auto-Rollback** bei Abweichung. Der Upgrade *hat* angewendet; ein Rollback
  im Fehlerfall würde eine zweite Zustandsänderung an eine Prüfung hängen, die
  gerade erst beweist, dass der Zustand nicht verstanden ist. Der Befehl steht in
  der Meldung, die Entscheidung bleibt beim Operator (§4.88: erst umschalten, wenn
  es funktioniert).
- **Kein `--image-tag`-Schalter.** `update` heißt „bring mich auf die
  veröffentlichte Version"; ein eigenes Image überschreibt es dabei absichtlich.
  Wer ein Dev-Image fährt, nimmt `helm upgrade` direkt (README Zeile 242).

## Fertig wenn

- `install.sh update` auf dem Live-Server das Image tatsächlich auf 0.1.91 bringt,
  nachgesehen am laufenden Pod, nicht am Chart.
- Ein künstlich gesetzter falscher Pin vom nächsten `update` korrigiert wird.
- Ein Abgleichsfehler zu Exit-Code != 0 führt (geprüft, nicht angenommen).
- `make check` grün, die vier S11-Prüfungen durch.
