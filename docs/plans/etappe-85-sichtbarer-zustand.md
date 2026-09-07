# Etappen 85–90 — Was die App nicht tut, muss sie sehen

## Die Grenze, die aus Etappe 84 folgt

> **Die App verwaltet den Homeserver. Das Skript verwaltet die App und den Cluster.**

`update` und `purge` gehören in die Shell: ein `helm upgrade` auf die eigene Release
ersetzt den Pod, der ihn ausführt, und ein „alles löschen"-Knopf in einer Weboberfläche
ist ein Fehlklick von einem Homeserver entfernt. Das ist keine Ausrede — der Preis ist,
dass die App diese Zustände trotzdem **sehen und benennen** muss, mit dem Befehl
daneben. Sonst heißt „die App kann das nicht" für den Operator „die App weiß nichts
davon".

## Befunde (nachgesehen, nicht vermutet)

| # | Befund | Beleg |
|---|---|---|
| 1 | **Der Rückweg existiert, der Moment dafür nicht.** Helm-Rollback ist seit langem eingebaut — nichts zeigt ihn an, wenn eine Release in `pending-upgrade` oder `failed` steht, also genau dann, wenn jeder weitere Befehl daran scheitert | `router.go:218` `POST /helm/releases/{name}/rollback` |
| 2 | **Kein Preflight.** Deploy und Upgrade laufen los und scheitern nach Minuten mit einer Textwand. Der server-seitige Dry-Run, den `doctor` seit E84 benutzt, könnte vorher laufen und die Ursache **vorher** nennen | `install.sh cmd_doctor`, `helm_setup.go` |
| 3 | **Die App kennt den Zustand von ESS, nicht den ihres Clusters.** Kein Endpunkt beantwortet „ist dieser Cluster in einem Zustand, in dem Operationen scheitern" | `/api/v1/status` liefert Komponenten, Nodes, Release |
| 4 | **Aufräumen kennt nur `Evicted`.** Auf diesem Cluster stehen zwei cert-manager-Pods seit 105 Tagen in `ContainerStatusUnknown` mit laufendem Ersatz daneben — durch den Filter gefallen | `DELETE /status/evicted-pods` · `kubectl get pods -A` |
| 5 | **Keine Komponenten-Tests.** `web` hat vitest, aber keine DOM-Bibliothek; UI-Änderungen sind typgeprüft und sonst nichts | offen seit E81 (§4.82) |
| 6 | Self-Update in der App braucht einen Job, der überlebt, was er ersetzt | offen seit E79 |

**Ausdrücklich geprüft und *kein* Problem** — damit niemand daran arbeitet: PVs stehen
auf `Delete`, es gibt keine CRDs, ESS-Secrets tragen kein `keep`, ClusterRole und
-Binding gehören Helm, und die 15 bzw. 10 Helm-Revisions-Secrets sind normale Retention.

## Etappen

| # | Titel | Größe | Hängt ab von |
|---|---|---|---|
| 85 | Der Grund steht vorher da, nicht nach drei Minuten | S | — |
| 86 | Der Moment für den Rückweg | S | 85 |
| 87 | Was `doctor` weiß, in der App | M | 85 |
| 88 | Aufräumen, das mehr kennt als „Evicted" | S | 87 |
| 89 | Komponenten-Tests | M | — |
| 90 | Self-Update per Job | M | offene Entscheidung |

### E85 — Preflight

Vor Deploy und Upgrade ein `helm upgrade --dry-run=server`. Rendert gegen den lebenden
Cluster und verweigert aus exakt den Gründen der echten Operation. Scheitert er, steht
der Grund im Assistenten, bevor irgendetwas angefasst wird.

Backend: die Dry-Run-Variante der vorhandenen Upgrade-Pfade, ein Ergebnis
`{ok, error}`. Frontend: der Knopf zeigt den Grund statt loszulaufen.

Warum zuerst: die schlimmste Erfahrung im Produkt ist ein dreiminütiger Rollout, der mit
einer Textwand endet — und die Ursache war die ganze Zeit in einer Sekunde ermittelbar.

### E86 — Der Rückweg zur richtigen Zeit

Release-Zustand sichtbar (`deployed` · `pending-upgrade` · `failed`), und in den beiden
letzten Fällen der Rollback-Knopf, den es seit langem gibt. Eine Fähigkeit, die nur
existiert, wenn man weiß, dass es sie gibt, ist für den Operator keine.

### E87 — Zustandsseite

Was `doctor` in der Shell zeigt, read-only in der App: Release-Zustände, Namespaces,
Volumes, Pods, die weder Running noch Completed sind. Für alles Zerstörerische **kein
Knopf, sondern der Befehl zum Kopieren** — die Grenze von oben, sichtbar gemacht statt
weggelassen.

### E88 — Aufräumen

`Evicted` ist ein Sonderfall von „dieser Pod ist tot und wird nie zurückkommen".
`ContainerStatusUnknown`, `Error`, `Completed` ohne Job — mit Alter und Grund, eine Zahl
und ein Knopf.

### E89 — Komponenten-Tests

Die Lücke, die ich in E81 offen benannt statt zugekleistert habe. vitest + DOM, und der
erste Test ist der Fehler von damals: ein abgeleiteter Wert, der nach dem ersten Render
ankommt, muss das Feld trotzdem erreichen.

### E90 — Self-Update

Weiterhin offen. Ein Job mit demselben ServiceAccount fährt das Upgrade und überlebt den
Pod, den es ersetzt; die UI wartet, bis die neue Version antwortet. Kostet bei einem
Fehlschlag den Panel-Zugang, braucht also einen Rückweg — und deine Entscheidung, ob der
kopierbare Befehl nicht reicht.

## Offene Entscheidungen

1. **Self-Update (E90):** Knopf, oder reicht `install.sh update`?
2. **Zerstörerisches in der App:** soll die Zustandsseite je Knöpfe bekommen (Namespace
   löschen, Release entfernen) — oder bleibt sie dauerhaft lesend, mit Befehlen zum
   Kopieren? Mein Vorschlag ist das Zweite.
3. **E89 jetzt oder später?** Es liefert dir sichtbar nichts und macht jede weitere
   UI-Etappe belastbarer.
