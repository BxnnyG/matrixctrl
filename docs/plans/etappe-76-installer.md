# Etappe 76 — Ein Installer, der die Fallen kennt (und ein Release, das ankommt)

## Der Auslöser

Der Operator hat auf einer frischen VM installiert. Sein Terminal, der Reihe nach:

1. `Error: INSTALLATION FAILED: Kubernetes cluster unreachable: Get "http://localhost:8080/version"`
   — `KUBECONFIG` war nicht gesetzt. k3s schreibt seine Kubeconfig nach
   `/etc/rancher/k3s/k3s.yaml`; ohne die Variable redet Helm mit niemandem. Die
   Fehlermeldung nennt einen Port, nicht die Ursache.
2. `helm uninstall` → `These resources were kept due to the resource policy:`
   PVCs + Secret. Danach `kubectl delete ns` — der Namespace verschwindet, die
   PVs nicht zwingend. Die Neuinstallation erbte eine Datenbank.
3. `kubectl get secret matrixctrl-secret -o jsonpath='{.data.admin-password}'`
   → leer. Weil der Chart 0.1.69 war: **v0.1.70 wurde nie veröffentlicht.**
4. `helm upgrade matrixctrl … --set secrets.adminPassword=…`
   → `non-absolute URLs should be in form of repo_name/path_to_chart, got: %E2%80%A6`
   — er hat das `…` aus dem README kopiert. Es stand da als Auslassungszeichen.
   Ein Kommandobeispiel, das man nicht ausführen kann, ist kein Kommandobeispiel.

Vier Fehlschläge, kein einziger davon in MatrixCtrl selbst. Alle vier im Weg
dorthin.

## Warum v0.1.70 nicht ankam

Der Release-Job ist an Schritt 6 gescheitert: *"CHANGELOG must have a section
for this version"*. Ich habe getaggt, ohne den CHANGELOG-Abschnitt zu schreiben.
Der Guard hat korrekt funktioniert — er stand nur an der falschen Stelle: **nach**
dem Tag. Ein Tag ist gepusht und damit öffentlich, bevor irgendetwas ihn prüft.

Der eigentliche Fehler ist meiner: ich habe letzte Runde berichtet, der Fix sei
ausgeliefert. Ausgeliefert war er in den lokalen Cluster. Veröffentlicht war er
nicht, und ich habe es nicht nachgesehen, weil `gh` fehlte — statt den einen
`curl` auf die GitHub-API zu machen, der zwei Sekunden dauert.

## Was gebaut wird

### 1. `scripts/install.sh` — ein Installer, der jede der vier Fallen kennt

Ein Skript, drei Unterbefehle, interaktiv wenn nichts angegeben wird:

    install    (Standard) Vorprüfung → Fragen → helm upgrade --install → warten → Passwort
    uninstall  entfernt auch das, was `helm uninstall` stehen lässt — nach Rückfrage
    password   liest das Admin-Passwort aus dem Secret
    status     Release, Pods, Ingress, URL

Vorprüfung, in dieser Reihenfolge, jede mit einer Aussage statt eines Codes:

- `kubectl`/`helm` vorhanden? Sonst: die exakten Installationsbefehle.
- Kubeconfig: `$KUBECONFIG`, dann `~/.kube/config`, dann
  `/etc/rancher/k3s/k3s.yaml` — gefunden, gesetzt, gesagt.
- Cluster erreichbar? Sonst: läuft k3s überhaupt (`systemctl is-active k3s`)?
- Ingress-Controller vorhanden (Traefik/nginx)? Sonst Warnung, kein Abbruch.
- **Reste einer vorherigen Installation**: PVC `matrixctrl-postgres` ohne
  Helm-Release. Das ist Falle 2, und sie wird gefragt, nicht geraten:
  Daten behalten oder löschen.

Fragen: Hostname · TLS-Modus · ESS-Namespace. Der TLS-Modus ist die Frage, die
der Operator gestellt hat („traefik … maby durch cloudflare mit proxied"), also
wird sie ausgeschrieben beantwortet:

| Modus | entrypoint / tls / certIssuer | wofür |
|---|---|---|
| `letsencrypt` | websecure · true · gewählter ClusterIssuer | cert-manager da, Port 80 offen, **kein** Cloudflare-Proxy (HTTP-01 scheitert hinter der orangen Wolke) |
| `cloudflare-full` | websecure · true · leer | Cloudflare proxied, SSL-Modus *Full*. Traefik zeigt sein Default-Zertifikat, Cloudflare akzeptiert es |
| `cloudflare-flexible` | web · false · leer | Cloudflare proxied, SSL-Modus *Flexible*. Origin spricht HTTP |
| `none` | web · false · leer | reines HTTP, LAN/Tunnel |

Die ClusterIssuer werden aufgelistet, nicht erfragt — wenn genau einer da ist,
ist er die Vorgabe.

Nach dem Rollout: URL, Benutzer, **Passwort**, und der DNS-Record, den es
braucht. Damit endet der Lauf da, wo der Operator hinwollte, statt bei
„deployed".

Läuft auch als `curl … | bash`: die Eingaben kommen aus `/dev/tty`, sonst würde
das Skript sich selbst als Antworten lesen.

### 2. Das Release reparieren

- `CHANGELOG.md`: Abschnitt für die Version, die tatsächlich veröffentlicht wird.
- Chart auf **0.1.71**. v0.1.70 wird nicht nachgezogen: der Tag existiert
  öffentlich, veröffentlicht wurde unter dieser Nummer nichts, und eine Lücke in
  der Versionsfolge ist ein ehrlicheres Protokoll als ein verschobener Tag.
  ROADMAP-Zeilen 74/75 werden entsprechend korrigiert — sie behaupten gerade
  eine Version, die es nicht gibt.
- `scripts/check-changelog.sh`, aufgerufen von `make check`: die Chart-Version
  braucht einen CHANGELOG-Abschnitt. Derselbe Guard wie in CI, nur **vor** dem
  Tag statt danach.

### 3. README

- Das `…` in `helm upgrade matrixctrl … --set …` durch ein ausführbares Kommando
  ersetzen. Im ganzen Dokument prüfen.
- Der Installer wird der empfohlene Weg; die rohen Helm-Kommandos bleiben
  darunter stehen.
- Ein Abschnitt „Deinstallieren", der sagt, was `helm uninstall` stehen lässt.

## Nicht in dieser Etappe

k3s oder Helm selbst installieren. Beides sind `curl | sh` aus fremden Quellen;
das Skript nennt die Befehle, führt sie aber nicht aus. Diese Entscheidung
gehört dem Operator (Regel 6).

## Gegenprobe

- `bash -n` auf das Skript, `helm template` mit jedem der vier TLS-Modi.
- `install` gegen den echten Cluster hier: es muss die laufende Installation als
  Upgrade erkennen und darf die vorhandenen Daten nicht anfassen.
- `password` muss dasselbe liefern wie der dokumentierte `kubectl`-Einzeiler.
- S11: ESS erreichbar · Config-Speichern zerstört nichts · Login · SFU-Patches.
