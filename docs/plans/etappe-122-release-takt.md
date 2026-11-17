# Etappe 122 — Release-Takt: `edge` und `stable`, Tag erst nach Abnahme

Stand 2026-11-17. Paket C aus `programm-2026-11.md`. Bisher wird jeder Tag `vX.Y.Z` sofort
`:latest` und jeder Installation als Update angeboten — abgenommen wurde danach. Ziel: Was
Produktion angeboten bekommt, ist vorher auf dem Testcluster gelaufen.

## Befund (gelesen)

- `release.yml` (Tag `v*`): Image `:X.Y.Z` **und** `:latest`, Chart `oci://…/charts/matrixctrl`,
  GitHub-Release. Prüft Chart.yaml `version`/`appVersion` == Tag, Doku-Pins == Tag,
  CHANGELOG-Abschnitt `## [X.Y.Z]`; am Ende `scripts/check-published.sh`.
- `internal/updatecheck` nimmt den höchsten Semver-Tag — **Vorabversionen eingeschlossen**:
  ein `0.1.128-rc.1` würde heute jeder Installation angeboten.
- Self-Update (`handlers/selfupdate.go:79`) lehnt alles außer `X.Y.Z` ab.
- Anzeige: `SelfUpdate.tsx` (Update-Karte), `VersionsCard.tsx`; API `GET /api/v1/version`.
- Schalter: `updateCheck.enabled` → `MATRIXCTRL_UPDATE_CHECK`.

## Betreiber-Entscheidungen (2026-11-17)

1. edge = Tag `vX.Y.Z-rc.N` → Image/Chart `X.Y.Z-rc.N` + beweglicher Tag `:edge`, GitHub
   als Pre-Release, `:latest` bleibt. stable = `vX.Y.Z` **auf demselben Commit** nach Abnahme.
2. stable wird vom selben Commit **neu gebaut** (Binary meldet `X.Y.Z`, gleicher Commit-Hash).
3. Kanal per Helm-Wert `updateCheck.channel: stable|edge` (Standard `stable`), Karte zeigt ihn an.

## Was gebaut wird

1. `updatecheck`: Kanal-Feld; `stable` überspringt Vorabversionen, `edge` nimmt sie mit.
   `Result.channel` in der API. Env `MATRIXCTRL_UPDATE_CHANNEL`, unbekannter Wert → `stable`
   mit Logzeile. Tests: beide Kanäle, rc < final, Installation auf rc im stable-Kanal.
2. Self-Update nimmt `X.Y.Z-rc.N` an (Regex), sonst unverändert.
3. Chart: `updateCheck.channel` → Env; Karte und VersionsCard zeigen „Kanal: stable/edge".
4. `release.yml`: Vorabversion erkannt → Tags `X.Y.Z-rc.N` + `edge`, kein `latest`,
   `--prerelease`. Stable → `X.Y.Z` + `latest` + `edge`. Chart.yaml/Doku-Pins/CHANGELOG
   werden bei rc gegen die **Basisversion** `X.Y.Z` geprüft (sonst ginge stable nicht vom
   selben Commit). `check-published.sh`: bei rc darf `latest` nicht auf den rc zeigen.
5. `docs/RELEASING.md`: neuer Ablauf (rc taggen → Testcluster per Self-Update → `make e2e` +
   vier Regressionschecks → stable taggen); CLAUDE.md „Ship by tag" angepasst.

## Gegenproben (müssen fallen)

- stable-Filter entfernt → Test „stable bietet rc nicht an" rot.
- Self-Update-Regex zurück → Test „rc als Ziel" rot.
- Workflow-Logik: Skript-Test für Tag→(Version, Basis, Kanal, Image-Tags) fällt bei Vertauschung.

## Grenzfälle

Kein Internet → wie bisher letzter guter Wert/Fehlertext (Kanal ändert das nicht). Nur
rc-Tags vorhanden im stable-Kanal → „kein Update", kein Fehler. ESS-Chart-Versionen
(`internal/helm/versions.go`) bleiben unberührt.

## Nicht in dieser Etappe

Kein Kanal-Schalter im Panel; kein Umetikettieren; keine automatischen edge-Builds pro Push.

## Fertig wenn

`make check` grün; Gegenproben fallen; `v0.1.128-rc.1` veröffentlicht, Testcluster (Kanal
edge) bietet es an und aktualisiert per Self-Update, `make e2e` + Regressionschecks grün;
danach `v0.1.128` vom selben Commit; `check-published.sh` für beide grün; ROADMAP/DESIGN
nachgezogen.
