# Etappe 84 — Der Lebenszyklus, den nur die Shell kennt

## Auslöser

> „bei mir ist mein remote jetz verbugget also von den alten versionen ess cluster iwi
> überbleibsel kann es nicht neu machen idk wie ich alles lösche maby ein delete all […]
> dann auf der node wo du gerade bist wie würde ein dau updates machen ein skript und
> dahinter update denke ich wäre am schönsten"

## Befund: „von vorne anfangen" hatte keinen Befehl

Nachgesehen auf dem laufenden Cluster, nicht vermutet:

| Versuch | Was bleibt |
|---|---|
| `helm uninstall matrixctrl` | beide PVCs + Secret (`resource-policy: keep`) |
| `helm uninstall ess` | `ess-postgres-data` 10Gi, `ess-synapse-media` 10Gi (ebenfalls `keep`) |
| `kubectl delete ns matrixctrl` | Namespace `ess` — den legt **unser** Chart an, mit `keep` |
| nochmal installieren | ein Upgrade auf allem, was oben liegen blieb |

Jede Entscheidung einzeln richtig, zusammen ein Cluster, der leer aussieht und sich
nicht so verhält. Zusätzlich geprüft und **nicht** problematisch: PVs stehen auf
`Delete` (Daten verschwinden mit dem PVC), keine CRDs, ESS-Secrets ohne `keep`,
ClusterRole/-Binding gehören Helm. Helm-Revisionen: 15 bzw. 10 — normale Retention,
kein Leck.

## Gebaut (ausgeliefert über `master`, ohne Release)

`scripts/install.sh` wird laut README von `master` geladen. Skript-Änderungen erreichen
Operatoren also beim Merge; Chart und Image sind unverändert, es gibt nichts zu taggen.

- **`doctor`** — Release-Zustände (`pending-upgrade` blockiert jeden weiteren Befehl,
  bis jemand zurückrollt), Namespaces in `Terminating`, Volumes, Pods die weder Running
  noch Completed sind, und ein server-seitiger Dry-Run als eigentliche Prüfung.
- **`purge`** — beide Releases, beide Namespaces, alle Volumes, cluster-weite RBAC.
  Listet vorher auf, verlangt getipptes „delete everything", von `--yes` nicht gedeckt.
- **`update`** — gibt es eine neuere, drauf damit. Übernimmt die bestehenden Werte,
  wartet auf den Rollout, und sagt bei Fehlschlag, dass die alte Version noch läuft.

### Der Fehler, den ich dabei gemacht habe

Die erste `doctor`-Fassung listete „Objekte, die Helm nicht übernehmen kann" als *jedes
Objekt ohne Helm-Label*. Gegen einen **gesunden** Cluster: dreißig Zeilen aus Pods,
cert-manager-Secrets, Helms eigenen Release-Secrets und CronJob-Jobs. Ein Check zu drei
Vierteln Rauschen — dieselbe Sache wie §4.75, §4.79 und §4.84, gebaut während diese
Einträge schon dastanden.

Ersetzt durch `helm upgrade --dry-run=server`: rendert gegen den lebenden Cluster und
verweigert aus exakt den Gründen einer echten Installation. Seine Fehlermeldung benennt
das Objekt — keine Heuristik kann das besser.

## Warum `update` ins Skript gehört und nicht in die App

Ein `helm upgrade` auf die eigene Release ersetzt den Pod, der ihn ausführt. Aus einer
Shell ist das kein Problem. Aus dem Cluster heraus bräuchte es einen Job, der überlebt,
was er ersetzt, plus einen Rückweg — eine eigene Etappe, weiterhin offen.

Daraus die Grenze für die nächste Runde:

> **Die App verwaltet den Homeserver. Das Skript verwaltet die App und den Cluster.**

Was die App *nicht* tut, muss sie trotzdem **sehen** und benennen können — mit dem
Befehl daneben. Das ist der Inhalt der Etappen 85 ff.
