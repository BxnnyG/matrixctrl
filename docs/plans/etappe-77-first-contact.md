# Etappe 77 — Was ein frischer Server beim ersten Kontakt zeigt

## Der Auslöser

Neuer Server, 0.1.71 frisch installiert. Der Operator, wörtlich:

> „ich geh auf die url anmelden geht aber dann something went wrong […] dann manuell
> auf setup gehen […] kein backup wieder einspielbar dann auf backup, hochgeladen
> geht nicht"

Drei Sätze, drei Fehler, alle auf demselben Weg: einloggen → orientieren →
Zustand wiederherstellen. Also genau die ersten drei Dinge, die jemand auf einem
neuen Server tut.

## Befund 1 — `Something went wrong!`

Das ist TanStack Routers Standard-`CatchBoundary`, wörtlich der String aus
`node_modules/@tanstack/react-router/dist/esm/CatchBoundary.js`. Er erscheint, wenn
eine Route eine unbehandelte Exception wirft, und er sagt nichts: keine Ursache,
kein Link, kein Weg weiter.

Die Kette dahinter, rückwärts:

1. `internal/api/handlers/status.go` fragt `ComponentHealth(ctx, h.essNS)` ab.
2. Auf einem frischen Server gibt es den Namespace `ess` noch nicht → Fehler.
3. Der Fehler wurde mit `_` verworfen. `components` blieb `nil`.
4. `nil` in einem `interface{}` serialisiert als `null`.
5. Das Dashboard rechnet `data.components.filter(...)` → TypeError.
6. React hängt die Route aus, Router zeigt seine Standardseite.

Jede Stufe für sich harmlos, und Stufe 3 ist die, die zählt: **der Fehler wurde in
dem Moment weggeworfen, in dem er das Einzige war, was es zu sagen gab.** Auf
einem laufenden Cluster ist das unsichtbar. Auf einem frischen ist es die ganze
Geschichte.

`null` dort, wo eine Liste versprochen ist, ist keine leere Antwort — es ist eine
anders geformte.

## Befund 2 — der Restore konnte im Pod nie funktionieren

    unlinkat /data/config-repo.restoring: read-only file system

`RestoreConfigRepo` legte das Archiv neben dem Repository ab (`root + ".restoring"`)
und tauschte, indem es `root` selbst umbenannte. Beides ist im Pod unmöglich:

- Das Config-PVC ist auf `/data/config-repo` gemountet, alles darüber ist das
  read-only Root-Dateisystem des Containers (`readOnlyRootFilesystem: true`).
- Ein Mountpoint lässt sich nicht umbenennen.

Der Fehler des Operators ist Schritt 1 von zweien, die beide nicht gehen. Die
Funktion hat in einer Produktionsumgebung noch nie gearbeitet.

**Warum kein Test das gesehen hat:** jeder einzelne übergab ein frisches
`t.TempDir()`. Dessen Elternverzeichnis ist beschreibbar. Das ist die eine
Anordnung, in der der Defekt nicht auftreten kann — dieselbe Form von blindem
Fleck wie Etappe 74, nur einen Stock tiefer: dort war es ein falscher Kommentar
über das Schema, hier eine stillschweigende Annahme über das Dateisystem.

## Was gebaut wird

### Restore innerhalb des Mounts

Staging und Sicherung wandern nach `<repo>/.matrixctrl-restore-new` und
`…-old`. Ablauf: extrahieren → Vorhandenes zur Seite schieben → Neues an seinen
Platz → aufräumen. Scheitert das Einsetzen, geht das Vorherige zurück; der
Operator steht dann da, wo er angefangen hat, statt vor einem leeren Repository.
Alles im selben Verzeichnis, also Renames statt Kopien, und nichts außerhalb des
beschreibbaren Volumes.

Ein Archiv, dessen Pfade auf die Staging-Namen fallen, wird abgelehnt statt
teilweise wiederhergestellt.

### Zwei Tests, und beide müssen gegen den alten Code fallen

- `TestRestoreConfigRepoKeepsTheDirectoryItWasGiven` — portabel: `os.SameFile`
  vor und nach dem Restore. Der alte Code benennt um, das Verzeichnis ist danach
  ein anderes. Prüft zusätzlich, dass im Elternverzeichnis nichts entsteht.
- `TestRestoreConfigRepoOnAReadOnlyParent` — die echte Pod-Anordnung, gebaut mit
  `syscall.Mount`: ein read-only Bind-Mount als Eltern, ein tmpfs als Volume
  darin. Überspringt sich ohne `CAP_SYS_ADMIN`. Er prüft die Anordnung erst
  selbst (Canary-Datei oben muss scheitern, unten gelingen), bevor er irgendetwas
  über den Restore behauptet — eine aus einer Fehlermeldung abgeleitete Annahme
  ist keine Reproduktion.

### Der Status-Endpunkt antwortet nie `null` auf eine Liste

Konkrete Typen statt `interface{}`, Fehler werden geloggt statt verworfen, und ein
neues Feld `unavailable` nennt die Quellen, die nicht geantwortet haben — weil
„nichts ausgerollt" und „konnte nicht nachsehen" in den Daten identisch aussehen
und das Gegenteil bedeuten.

Test: ein Zero-Value-Handler *ist* die Frischinstallation (kein Cluster-Client,
kein Helm-Client, nichts erreichbar). Die Antwort muss trotzdem ein gültiges
Dashboard-Payload sein.

### Das Dashboard eines leeren Servers

Kein Release und keine Komponenten → nicht vier ehrliche Nullen, die wie ein
Ausfall aussehen, sondern die Aussage „hier läuft noch kein ESS" und ein Knopf
nach Setup. Sind Quellen unerreichbar, sagt es das stattdessen — anderer Text,
anderes Icon, denn es ist ein anderer Zustand.

### Und eine Fehlerseite, die etwas sagt

`defaultErrorComponent` am Router: Fehlermeldung im Klartext und Links nach
Übersicht, Setup, System & Logs. Was auch immer kaputt ist — der Bildschirm nennt
es und bietet einen Weg weiter.

## Gegenprobe

- Beide Restore-Tests und der Status-Test gegen die alte Implementierung: müssen
  fallen. (Sie tun es; der read-only-Test reproduziert die Fehlermeldung des
  Operators wörtlich.)
- `make check`, danach Image bauen, ausrollen, S11.
