# Orchestrator-Modus (Token-Disziplin)

Der Hauptkontext ist teuer: Er wird bei jedem Turn komplett mitgeschickt. Ziel: ihn klein
halten, Arbeit in isolierte Subagents auslagern, nur Ergebnisse zurückholen.

## Grundregeln
1. Hauptkontext = Plan + Entscheidungen + Ergebnisse. Keine Datei-Dumps, keine langen Logs,
   keine kompletten Diffs.
2. Delegieren, sobald eine Aufgabe mehr als ~3 Dateien lesen/durchsuchen erfordert, lange
   Ausgaben erzeugt (Tests, Builds, Logs, Repo-weites `grep`) oder in sich abgeschlossen ist.
3. Selbst machen, wenn es schneller ist als das Briefing: einzelne bekannte Datei, kleine
   Edits, kurze Befehle. Ein Subagent startet kalt und kostet ebenfalls.
4. Kein doppeltes Lesen: Was ein Subagent zusammengefasst hat, wird nicht nochmal gelesen.

## Subagent-Briefing (Pflichtformat)
```
ZIEL: <ein Satz>
KONTEXT: <nur relevante Fakten, Pfade, Konventionen – max. ~10 Zeilen>
GRENZEN: <was nicht angefasst wird>
FERTIG WENN: <prüfbares Kriterium, z. B. make check grün>
RÜCKGABE: max. 15 Zeilen – geänderte Dateien, Ergebnis, offene Punkte.
          Keine Code-Blöcke, keine Logs, außer bei Fehlern (nur die relevanten Zeilen).
```

## Modellwahl
- Suchen, Lesen, Zusammenfassen, Logs auswerten → `scout` (haiku) bzw. `Explore`.
- Implementierung, Refactoring → `worker` (sonnet).
- Architekturentscheidungen → im Hauptkontext, auf Basis der Zusammenfassungen.

## Token-Sparsamkeit
- Ausgaben immer begrenzen (`| head`, `| tail`, `grep -c`, `-q`).
- Tests: nur Fehlschläge zeigen (`go test ./... 2>&1 | grep -E "^(--- FAIL|FAIL|ok)" | grep -v "^ok"`,
  `npx vitest run 2>&1 | grep -E "Tests |FAIL|×"`).
- Dateien gezielt lesen (Zeilenbereiche).
- Wiederkehrende Analysen als Skript in `scripts/`.

## Projektgedächtnis statt Chat-Gedächtnis
- Dauerhaftes Wissen → `CLAUDE.md` / `docs/`. Laufender Stand → `HANDOFF.md`
  (Ziel · Erledigt · Offen/Nächster Schritt · Entscheidungen · Relevante Dateien),
  nach jedem abgeschlossenen Teilschritt aktualisieren.
- Das Repo ist öffentlich: In `CLAUDE.md` und `HANDOFF.md` keine IPs, Zugangswege,
  Personennamen oder Geheimnisse — die gehören ins private Assistenten-Gedächtnis.

## Kontext-Hygiene
- Bei großem Kontext oder abgeschlossenem Teilziel: `HANDOFF.md` aktualisieren und dem
  Betreiber `/compact` (gleiches Thema) bzw. `/clear` (Themenwechsel) vorschlagen.
- Neuer Chat: `CLAUDE.md` (automatisch) + `HANDOFF.md` lesen, nicht das ganze Repo.

## Antwortstil
Kurz. Plan als Stichpunkte, Ergebnisse als Zusammenfassung. Subagent-Ergebnisse nicht in
voller Länge wiederholen.
