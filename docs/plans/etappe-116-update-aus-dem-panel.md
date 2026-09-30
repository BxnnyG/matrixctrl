# Etappe 116 — MatrixCtrl aus dem Panel aktualisieren

Stand 2026-09-30. Vorgezogen auf Wunsch des Betreibers; Föderation, Worker-Insights und
Bridges rücken auf 117–119.

## Entscheidung des Betreibers (2026-09-30)

**Komplettes Helm-Upgrade aus dem Panel** — auch Updates, die Berechtigungen ändern. Dafür
darf MatrixCtrl seine eigenen Rollen ändern. Zur Wahl standen auch „nur das Programm
tauschen" (kein Weg zu neuen Rechten) und „so lassen". Die Folge ist ausgesprochen: eine
Rolle, die ihr eigener Inhaber erweitern darf (`escalate`), ist im Ergebnis Cluster-Admin.
Ein übernommenes Panel könnte den Server übernehmen. §4.38 (E40) hatte genau das entfernt;
diese Etappe setzt es bewusst und begrenzt wieder ein.

## Die Falle, die das Design bestimmt

MatrixCtrl läuft mit `strategy: Recreate` (die Postgres-Beiwagen-PVC ist RWO). Bei einem
Upgrade beendet Kubernetes **zuerst** den alten Pod — den, in dem das Upgrade liefe. Helm
würde mitten im Warten abgeschossen, das Release bliebe `pending-upgrade` (§4.88), und
niemand wäre da, um zurückzurollen.

**Also läuft das Upgrade in einem eigenen Job.** Er gehört nicht zum Deployment, überlebt den
Neustart, wartet auf den neuen Pod und rollt bei Misserfolg selbst zurück (`atomic`).

## Was gebaut wird

1. **`internal/selfupdate`** — die Werte des laufenden Releases übernehmen, ohne
   `image.tag` (wie `install.sh carry_values`, sonst bliebe das Update auf der alten
   Version stehen), das Chart aus `oci://ghcr.io/bxnnyg/charts/matrixctrl` ziehen,
   `upgrade --wait --atomic`, 5 Minuten.
2. **Unterbefehl `matrixctrl self-update --version X`** im selben Binary — der Job führt
   ihn aus, mit dem **laufenden** Image (das sicher vorhanden ist) und dem
   ServiceAccount `matrixctrl`.
3. **API:** `POST /api/v1/self-update {version}` legt den Job an (einer zur Zeit);
   `GET /api/v1/self-update` liefert Zustand und Log des letzten — nach dem Neustart
   beantwortet das die **neue** Instanz.
4. **Oberfläche:** in der Versionsanzeige „Jetzt aktualisieren" mit Bestätigung; danach eine
   Übersicht „MatrixCtrl aktualisiert sich — kurz nicht erreichbar", die fragt, bis die neue
   Version antwortet, und dann die Seite neu lädt (das alte JavaScript passt nicht zum
   neuen Server).
5. **Rechte (Chart):**
   - eigener Namespace (`matrixctrl-self`): Secrets (Helm-Speicher), Deployments, Services,
     ServiceAccounts, PVCs, Ingresses, Jobs, `pods/log`; Rollen und Bindungen — `escalate`
     und `bind` nur für `matrixctrl-self`.
   - ESS-Namespace: Rolle und Bindung `matrixctrl` ändern — `escalate`/`bind` nur für
     `matrixctrl`.
   - Cluster: ClusterRole und ClusterRoleBinding `matrixctrl` ändern — `escalate`/`bind`
     nur für `matrixctrl`; der ESS-Namespace ist ein Objekt des Charts und braucht `patch`.
   - **Nicht:** `create` auf ClusterRoles. Bringt ein Update eine *neue* Rolle mit anderem
     Namen, scheitert der Job, rollt zurück, und die Oberfläche sagt: dieses Update per
     `install.sh`. Die Grenze ist ehrlich, und sie ist selten.
6. `ForbiddenAlways` bleibt wahr: „`escalate` auf Rollen" ohne Namen ist weiter verweigert —
   nur die eigenen, benannten Rollen sind erlaubt. Die Rechte-Übersicht bekommt Namen.

## Einmalig

Die neuen Rechte kommen mit dem Chart. Das erste Update auf diese Version geht also noch per
`install.sh update`; ab dann aus dem Panel.

## Fertig wenn

- Die Werte-Übernahme entfernt `image.tag` und lässt `image.repository` stehen, ohne einen
  leeren `image:`-Block zu hinterlassen (Test, gleiche Fälle wie `install.sh`).
- Der Job läuft unter dem eigenen ServiceAccount mit dem laufenden Image, einer zur Zeit
  (Test).
- `kubectl auth can-i escalate roles -n ess` bleibt `no`, `… roles/matrixctrl` ist `yes`
  (Doctor-Gegenprobe).
- Live: Update auf dem alten Server aus dem Panel, neue Version läuft, Release `deployed`.
