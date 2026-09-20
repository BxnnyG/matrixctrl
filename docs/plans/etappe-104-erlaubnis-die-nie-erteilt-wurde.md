# Etappe 104 — Die Erlaubnis, die nie erteilt wurde

> „dann warum ist hochgeladene dateien einschliessen nicht auswählbar"

Weil MatrixCtrl es nicht darf. Nicht „kann nicht", sondern **darf nicht** — und das
steht sogar so im eigenen Sicherheitsmodell.

## Was gemessen wurde

Gegen den echten Dienstaccount, nicht gegen meine Kubeconfig:

```
$ kubectl auth can-i --as=system:serviceaccount:matrixctrl:matrixctrl -n ess ...
get    pods        yes
get    pods/log    yes
create pods/exec   no        ← hier bricht es
```

Der Ablauf in `BackupSizes`: `DirSizeInPod` → exec → 403 → `media_available: false`
→ `disabled={sizes?.media_available === false}` → graues Kästchen. Die Oberfläche
ist korrekt; sie zeigt wahrheitsgemäß an, dass die Funktion nicht verfügbar ist.

## Warum das schlimmer ist als ein fehlendes Recht

`internal/k8s/permissions.go`, Zeile 161:

```go
var ForbiddenAlways = []Permission{
    ...
    {Group: "", Resource: "pods", Subresource: "exec", Verb: "create",
     Namespaced: true, Why: "shell into any managed pod"},
```

`pods/exec` fehlt nicht nur in den Rechten — es steht auf der **Verbotsliste**,
zusammen mit `impersonate` und `clusterroles create`. Daneben der Kommentar: *„these
must stay denied — if any becomes allowed, the wildcard has grown back."*

Etappe 102 hat also eine Funktion gebaut, deren Berechtigung dasselbe Repository an
anderer Stelle ausdrücklich als Sicherheitsverletzung führt. Hätte ich nur das Chart
erweitert, hätte der eigene Prüftest Alarm geschlagen — zu Recht.

## Der eigentliche Fehler: falsch verifiziert

In Etappe 102 steht als Beleg: *„Verified live: 290 files, 37.2 MB streamed out of
the running pod."* Das war wahr. Und wertlos.

Der Test lief aus meiner Shell mit `/root/.kube/config` — **cluster-admin**. Die
Anwendung läuft als `system:serviceaccount:matrixctrl:matrixctrl`. Ich habe
gemessen, ob *ich* in den Pod komme, und daraus geschlossen, dass *sie* es kann.

Das ist die achte Wiederholung des Musters (§4.79, §4.84, §4.90, §4.92, §4.95,
§4.99, §4.103) und die bisher unangenehmste Variante. Die bisherigen sieben waren
„gebaut, benannt, nicht verdrahtet". Diese hier ist eine Stufe fieser:

**Merksatz:** *Eine Prüfung, die als die falsche Identität läuft, prüft nichts.*
Verifikation muss die Bedingungen des echten Aufrufers haben — sonst beweist sie
nur, dass der Prüfer mehr darf als das Programm.

## Und die dritte Ebene

`clusterrole.yaml` behauptet über fehlende Rechte:

> which the permission matrix in `internal/k8s/permissions.go` reports rather than
> leaving to be discovered halfway through an install

`RequiredPermissions` wird von **keinem** Produktionspfad gelesen — nur von
`permissions_live_test.go`, das ohne `RUN_LIVE=1` übersprungen wird und, wenn es
läuft, dieselbe falsche Identität fragt. Es gibt keine Matrix, die irgendetwas
meldet. Die Zusicherung existiert als Kommentar.

## Was gebaut wird

1. **Das Recht wird erteilt — namespaced.** `pods/exec` (`create`) kommt in die
   **Role im ESS-Namespace**, nicht in die ClusterRole (§4.38: was in einen
   Namespace passt, gehört dorthin). Ohne `resourceNames`: der Pod wird über
   `app.kubernetes.io/name=synapse-main` gesucht und heißt generiert, eine
   Namensliste wäre genau die Sorte stiller Bruch, um die es hier geht.
2. **Der Eintrag wandert sichtbar.** Aus `ForbiddenAlways` in
   `RequiredPermissions`, mit der Entscheidung des Operators im Kommentar
   („Darf MatrixCtrl in Pods hineingreifen: JA!", 2026-09-20) und mit dem, was
   damit eingekauft wird: wer in Synapse exec darf, kann dort alles lesen.
   Gelöscht wird nichts — das Muster dieser Datei ist, dass eine verschwindende
   Zusicherung sich bemerkbar macht.
3. **Ein Test, der die Identität prüft, bevor er das Recht prüft.** Der Live-Test
   stellt zuerst fest, als wer er läuft, und schlägt fehl statt zu bestehen, wenn
   das nicht der Dienstaccount ist. Ein grüner Lauf unter cluster-admin ist die
   Zusicherung, die diese Etappe verursacht hat.
4. **`doctor` fragt es von außen richtig.** `kubectl auth can-i --as=<sa>` über die
   erforderlichen Rechte — das ist der billigste Weg, die richtige Identität zu
   fragen, ohne im Pod zu sein, und `doctor` ist die Stelle, an der ein Operator
   ohnehin nachsieht.
5. **Die Oberfläche nennt den Grund.** Statt „zurzeit nicht lesbar" bei einem 403:
   dass die Berechtigung fehlt und dass ein Upgrade sie mitbringt. Ein graues
   Kästchen ohne Grund ist die Frage, die diese Etappe ausgelöst hat.

## Bewusst nicht

- **Keine ClusterRole-Erweiterung.** Exec im ESS-Namespace ist die Anforderung;
  exec überall ist es nicht.
- **Keine Berechtigungs-Matrix-Seite in der Oberfläche.** Wäre richtig, ist aber
  eine eigene Etappe — hier wird der eine Fall gemeldet, der jetzt jemanden
  blockiert, nicht ein Rahmen für alle.

## Fertig wenn

- `kubectl auth can-i --as=system:serviceaccount:matrixctrl:matrixctrl -n ess
  create pods/exec` → **yes**, nach dem Upgrade, am echten Cluster.
- Das Kästchen „Hochgeladene Dateien einschließen" ist anklickbar und zeigt die
  gemessene Größe.
- Ein Archiv **mit** Medien lädt durch und enthält `media/media.tar`.
- Der Live-Test verweigert den grünen Lauf unter der falschen Identität.
- `make check` grün, die vier S11-Prüfungen durch.
