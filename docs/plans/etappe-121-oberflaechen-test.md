# Etappe 121 — Oberflächen-Test gegen den Testcluster

Stand 2026-11-14. Paket B aus `programm-2026-11.md`. Die schlimmsten Fehler der letzten
Releases fand der Betreiber per Screenshot, kein Test. Es fehlt ein Lauf, der sich wie ein
Mensch anmeldet, jede Seite öffnet und eine Änderung bis auf den Cluster durchzieht.

## Befund (gelesen)

- `web/scripts/verify-ui.mjs` (Playwright, Chromium liegt bereits im Cache): öffnet Seiten,
  fällt bei Console-Fehlern, gescheiterten Requests, leerem `#root`, Umleitung auf
  `/auth/login`, sensiblen Mustern; Skips sind ohne `--allow-skip` ein Fehlschlag (§4.48).
- **Routenliste hartkodiert und schon wieder gedriftet** (Z. 154–174): 11 Pfade; es fehlen
  `/workers`, `/tls-dns`, `/mail`, `/login-providers`, `/federation`, `/bridges`, `/backup`,
  `/helm/upgrade`. Dynamisch und nie geprüft: `/hooks/$id`, `/config/$slice`, `/$id`.
  Genau diese Drift hatte §4.48 schon einmal behoben, durch Disziplin statt Mechanik.
- Anmeldung wird übersprungen: Token per `MATRIXCTRL_TOKEN` in localStorage. Das echte
  Formular (`routes/auth/login.tsx`, POST `/api/v1/auth/bootstrap/login`, Nutzer `admin`,
  Passwort aus Chart-Secret `<name>-secret`, Key `admin-password`) prüft niemand.
- Schreibweg: Aufgaben-Ansicht (`internal/tasks/tasks.go`, POST `/api/v1/config/tasks`) →
  `ApplyBar` „Übernehmen" → Vorschau → Commit → Helm-Upgrade. Kein Test fährt ihn.
- Kein `make e2e`.

## Was gebaut wird

1. **Gemeinsamer Kern** `web/scripts/lib/ui-check.mjs`: Seitenprüfung (Fehlerkriterien,
   Redaction, Screenshot) aus `verify-ui.mjs` herausgelöst; `verify-ui` und `e2e` nutzen ihn
   (S12, keine Kopie).
2. **Routen aus dem Router statt aus einer Liste**: Die Pfade werden aus
   `web/src/routeTree.gen.ts` gelesen. Dynamische Routen brauchen einen Auflöser (erste
   Zeile der zugehörigen Liste, z. B. erster Raum, erster Konfig-Abschnitt) oder einen
   Ausschluss mit Begründung. Eine neue Route ohne beides lässt den Lauf fallen.
3. **`web/scripts/e2e.mjs`**: (a) Login über das Formular, (b) jede Route über den Kern,
   (c) Schreibschritt: Einstellungen → „Name der App" auf einen Testwert → Übernehmen →
   warten bis fertig → Wert auf dem Cluster nachweisen (Element-Web-`config.json` bzw.
   Helm-Revision +1) → alten Wert zurück → Übernehmen. Zurücksetzen auch im Fehlerfall
   (`finally`), und der Lauf weigert sich zu starten, wenn noch ungespeicherte/unübernommene
   Änderungen anstehen (fremde Arbeit wird nicht mit übernommen).
4. **`make e2e BASE=…`**: Passwort aus `MATRIXCTRL_ADMIN_PASSWORD`; fehlt es, liest das Target
   es aus dem Secret des lokalen Testclusters. Nie gegen Produktion gedacht; der Schreibschritt
   lässt sich mit `E2E_READONLY=1` abschalten.

## Gegenproben (müssen fallen)

- Eine Seite wirft beim Rendern (lokaler Build gegen den Testcluster) → Lauf rot.
- Neue Route ohne Auflöser/Ausschluss → Lauf rot.
- Übernehmen ohne Wirkung (erwarteter Wert stimmt nicht) → Lauf rot.
- Falsches Passwort → Lauf rot, nicht „übersprungen".

## Nicht in dieser Etappe

Keine Golden-Image-Vergleiche (Begründung in `verify-ui.mjs`), kein OIDC-Login im Lauf
(braucht einen Test-IdP; Bootstrap-Modus reicht für den Testcluster), keine CI-Ausführung
(die CI hat keinen Cluster).

## Fertig wenn

`make e2e` ist auf dem Testcluster grün; alle vier Gegenproben fallen; DESIGN.md (S9 + neuer
§4-Eintrag), PROZESS §4 und ROADMAP nennen `make e2e`; `make check` grün.
