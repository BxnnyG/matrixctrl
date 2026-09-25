# Etappe 105 — Eine Frage, die nur „nein" sagen kann

Etappe 104 hat das Recht erteilt, das der Medien-Export braucht. Es ist erteilt. Und
`doctor` sagt weiter, es fehlt:

```
$ scripts/install.sh doctor
Can MatrixCtrl do what it needs?
✓ get pods
✓ get pods/log
! create pods/exec — denied (read the media volume for backups)
      A denial here means the feature is off, not broken. The role ships with
      the chart: scripts/install.sh update
```

Der Operator soll `update` laufen lassen. Er läuft schon auf dem Chart, das das Recht
mitbringt. Er kann das beliebig oft tun.

## Was gemessen wurde

Dieselbe Frage, dreimal, gegen denselben Cluster, gegen denselben Dienstaccount:

```
kubectl auth can-i --as=$SA -n ess create pods/exec              → no
kubectl auth can-i --as=$SA -n ess create pods --subresource=exec → yes
SubjectAccessReview {resource: pods, subresource: exec, verb: create}
  → allowed: true, "RBAC: allowed by RoleBinding matrixctrl/ess"
```

Und das Recht ist wirklich da — der Live-Test, der als der Dienstaccount fragt:

```
running as "system:admin", which is not "system:serviceaccount:matrixctrl:matrixctrl"
  — impersonating the service account
checked 91 required permissions in namespace "ess"
--- PASS
```

91 von 91. Vor Etappe 104 war es 90 von 91, und das eine war genau dieses. Die
Funktion ist frei; die Meldung ist falsch.

## Warum

`kubectl auth can-i VERB TYPE/NAME` — der Schrägstrich trennt **Typ und Namen**, nicht
Ressource und Unterressource. `create pods/exec` fragt also:

> Darf dieser Dienstaccount **einen Pod namens `exec` anlegen**?

Nein, darf er nicht: `create` auf `pods` steht nicht in der Role (bewusst — MatrixCtrl
legt keine Pods an, das tut Helm über Deployments). Die Antwort ist völlig korrekt. Sie
gehört zu einer anderen Frage. Für Unterressourcen heißt es `--subresource=exec`; das
Beispiel steht sogar in `kubectl auth can-i --help`.

Dass `get pods/log` daneben ein ✓ bekommt, ist keine Entlastung, sondern der Beweis:
`get` auf `pods` **ist** erteilt, ohne `resourceNames`, also darf man auch „den Pod
namens `log`" lesen. Zwei Zeilen mit derselben Fehlform, eine zufällig grün, eine
zufällig rot. Keine der beiden hat etwas über Unterressourcen gesagt.

## Der eigentliche Fehler: der Beleg hatte keine Aussagekraft

In §4.104 steht als Beleg für die Diagnose:

```
create pods/exec   no        ← hier bricht es
```

Diese Zeile druckt `no`, **ob das Recht erteilt ist oder nicht**. Sie war schon damals
kein Beweis. Die Diagnose war trotzdem richtig — aber sie kam aus dem Go-Test
(„1 required permission(s) denied"), nicht aus dieser Zeile. Ein Beleg, der bei der
Gegenthese gleich aussieht, ist Dekoration.

### Und es stand schon geschrieben — seit Etappe 37

Das Gate aus Punkt 6 hat beim ersten Lauf drei Treffer gemeldet. Einer davon ist
`docs/plans/etappe-37-rbac-scoping.md`, geschrieben lange vor dieser Etappe:

> „…jeder Eintrag als `SubjectAccessReview` an den API-Server gefragt — **die
> maßgebliche Form, da `kubectl auth can-i pods/log` Unterressourcen anders liest und
> auf eine `serviceaccounts/token`-Frage `yes` antwortete, die der API-Server mit
> `false` beantwortet.**"

Entdeckt, gemessen, aufgeschrieben. Mit demselben Beispiel, auf das ich hier
unabhängig als Gegenprobe gekommen bin. Danach wurde in Etappe 104 ein
Operator-Werkzeug gebaut, das genau diese Form benutzt.

Das ist die dritte Etappe in Folge, in der die richtige Überlegung bereits im
Repository stand, als der Fehler gemacht wurde: §4.103 (die Begründung gegen
`--reuse-values` stand über dem Code, der denselben Fehler machte), §4.104 („pass
trivially against a cluster-admin binding" stand über dem Test), und jetzt diese.

Daraus folgt die Form, die dieser Etappe wichtiger ist als die Korrektur selbst:
**aufgeschriebenes Wissen hat keine Wirkung.** Ein Satz in einem Plan von vor
67 Etappen hindert niemanden. Eine Prüfung, die fehlschlägt, schon. Deshalb ist
Punkt 6 kein Zusatz, sondern der eigentliche Ertrag — der Satz aus Etappe 37 ist
jetzt ein Gate.

Das ist die **neunte** Wiederholung des Musters (§4.79, §4.84, §4.90, §4.92, §4.95,
§4.99, §4.103, §4.104) und die zweite Stufe derselben Unterart:

| | Prüfung läuft | Frage |
|---|---|---|
| §4.104 | grün | über das falsche **Subjekt** („kann *ich* das?") |
| §4.105 | rot | über das falsche **Objekt** („einen Pod namens exec anlegen?") |

Etappe 104 hat repariert, *wer* gefragt wird. Diese repariert, *was* gefragt wird. Was
beide verbindet: ein Ergebnis wurde geglaubt, ohne zu prüfen, ob es auch anders hätte
ausfallen können.

**Merksatz:** *Eine Prüfung, die bei der Gegenthese dasselbe ausgibt, hat nichts
gemessen. Bevor man eine Antwort glaubt, muss klar sein, wie das Gegenteil aussähe.*

## Was gebaut wird

1. **`doctor` fragt in der richtigen Form.** `--subresource=` für `pods/exec` und
   `pods/log`, angezeigt weiterhin als `create pods/exec`, damit die Zeile lesbar
   bleibt.

2. **Eine Gegenprobe, die stumm bleibt, wenn alles stimmt.** Zusätzlich wird ein Recht
   gefragt, das **verweigert werden muss**: `create serviceaccounts
   --subresource=token` (steht in `ForbiddenAlways` — es würde Tokens fremder Konten
   ausstellen). Gemessen:

   | Lage | Antwort | erkennt |
   |---|---|---|
   | gesund | `no` | — (keine Ausgabe) |
   | Fehlform `serviceaccounts/token` | `yes` | genau den Fehler dieser Etappe: `create serviceaccounts` **ist** erteilt |
   | `--as` wirkt nicht | `yes` | die Fehler der Etappe 104, von außen |
   | Role zu weit | `yes` | eine echte Rechteausweitung |

   Ein Ja hier heißt: **jedes ✓ darüber ist bedeutungslos** — und genau das sagt die
   Meldung dann, statt weiter fünf Zeilen Zuversicht zu drucken.

3. **`--subresource` wird nicht vorausgesetzt, sondern geprüft.** Kann das vorhandene
   `kubectl` den Schalter nicht, sagt `doctor`, dass es die Frage hier nicht stellen
   kann. Eine unbeantwortbare Frage ist kein „nein".

4. **Der Live-Test des Medien-Exports spricht als der Dienstaccount.** Die Zahlen
   „290 Dateien, 37,2 MB", die in §4.102 als Beweis stehen, kamen aus einer
   Root-Shell. `TestLiveTarFromPod` geht durch dasselbe `asServiceAccount`, das
   Etappe 104 für die Rechteprüfung gebaut hat — und dann auch für SPDY gilt, nicht
   nur für SubjectAccessReviews.

5. **Und dort ebenfalls eine Gegenprobe:** derselbe Exec-Versuch als
   `system:serviceaccount:matrixctrl:default` **muss** mit 403 scheitern. Sonst tragen
   die Impersonations-Kopfzeilen nicht in den Exec-Pfad, der Erfolg oben käme wieder
   von cluster-admin, und wir stünden genau da, wo Etappe 104 angefangen hat.

6. **Ein Gate, damit die Fehlform nicht zurückkommt.** `check-commands.sh` prüft schon,
   dass jeder Befehl in den Dokumenten *ausführbar* ist (das `…`-Problem). Ein Befehl,
   der läuft und eine andere Frage beantwortet, ist derselbe Verrat am Leser. Also:
   `auth can-i … <typ>/<unterressource>` für die bekannten Unterressourcen (`exec`,
   `log`, `token`, `scale`, `status`, `portforward`, `attach`, `finalize`, `proxy`)
   schlägt fehl — in `README.md`, `docs/**` **und** in `scripts/*.sh`.

## Randfälle

- **Nichts installiert:** unverändert, der Block wird übersprungen (kein Dienstaccount,
  über den man reden könnte).
- **Kein `impersonate`-Recht in der Kubeconfig:** `--as` scheitert dann für *alle*
  Fragen. Die Gegenprobe bleibt `no`, also stumm — deshalb wird zusätzlich der erste
  Aufruf auf einen Fehlerausgang geprüft und als „konnte nicht gefragt werden"
  gemeldet, nicht als Verweigerung.
- **Alter `kubectl`:** siehe 3.
- **ESS in anderem Namespace / anderer Release-Name:** der Block fragt weiter gegen
  `$ESS_NAMESPACE`, das aus der Instanz kommt.
- **Kein Synapse-Pod (frischer Cluster):** der Live-Test überspringt sich bereits
  selbst; die Gegenprobe braucht ihn nicht, ein 403 kommt vor dem Pod.

## Bewusst nicht

- **Keine Rechte-Matrix in der Oberfläche.** Steht schon in Etappe 104 als eigene
  Etappe zurückgestellt; hier wird nur die Frage richtig gestellt.
- **`doctor` liest die 91 Rechte nicht aus dem Binary.** Verlockend (eine Liste statt
  zwei), aber `doctor` muss auch dann etwas sagen können, wenn der Pod nicht läuft —
  und das ist der Fall, in dem man `doctor` benutzt. Die fünf Zeilen bleiben die
  operatornahe Auswahl; die Gegenprobe ist der Ersatz für die fehlende Kopplung.
- **Kein Bugfix in der Anwendung.** Es gibt keinen: das Produkt darf exec, seit
  0.1.92, und tut es. Falsch war ausschließlich die Diagnose *darüber*.

## Fertig wenn

- `doctor` zeigt am echten Cluster `✓ create pods/exec`, **ohne dass ein Recht
  geändert wurde** — der Unterschied liegt allein in der Frage.
- Die Gegenprobe feuert, wenn man sie absichtlich kaputt macht (Fehlform, kein `--as`),
  und bleibt sonst unsichtbar.
- `TestLiveTarFromPod` läuft als Dienstaccount grün und als
  `matrixctrl:default` rot (403).
- `check-commands.sh` schlägt bei einer eingesetzten Fehlform fehl und ist danach grün.
- §4.104 bekommt einen Nachtrag, der die dort abgedruckte Zeile als nicht aussagekräftig
  kennzeichnet — stehen bleibt sie, überschrieben wird hier nichts.
- `make check` grün, die vier S11-Prüfungen durch.
