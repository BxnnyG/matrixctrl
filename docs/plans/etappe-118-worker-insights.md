# Etappe 118 — Worker-Insights: braucht Synapse Worker, und welchen?

Stand 2026-10-08. Menüpunkt seit Phase 1 ausgegraut; ROADMAP Phase 5: „per-worker load and
scaling recommendations".

## Befund (gemessen, nicht vermutet)

- ESS 26.9.3 kennt **23 Worker-Typen** (`synapse.workers.<typ>.enabled/replicas`), zwölf
  davon auf eine Instanz begrenzt. Jeder Worker ist ein eigener Synapse-Prozess mit eigenem
  Speicher.
- Jeder Synapse-Prozess liefert Prometheus-Werte auf Port 9001 (`synapse-metrics`), darunter
  `process_cpu_seconds_total`, CPU-Zeit **pro Endpunkt** (`synapse_http_server_response_ru_*`
  nach `servlet`) und pro Hintergrundjob, sowie Föderations-Warteschlangen.
- **Produktion, gelesen 2026-10-08:** ~3 080 CPU-Sekunden seit dem Start vor gut acht Tagen
  — unter 1 % eines Kerns. Größter zuordenbarer Posten: eingehende Föderation
  (`FederationSendServlet`). Endpunkte + Hintergrundjobs erklären nur **~15 %** der
  CPU-Zeit; der Rest ist Replikation, Event-Schleife, Datenbank.

Folgen für die Seite: Die Hauptzahl ist die **Gesamt-CPU je Prozess**. Die Aufteilung ist
ein Hinweis, mit dem nicht zuordenbaren Rest ausgewiesen. Und „kein Worker nötig" muss ein
vollwertiges Ergebnis sein — für die allermeisten Installationen ist es das richtige.

## Was gebaut wird

1. **`internal/synmetrics`**: liest die Prometheus-Ausgabe eines Prozesses
   (`prometheus/common/expfmt`) in eine Momentaufnahme; aus zwei Aufnahmen werden Raten
   (Kerne), mit Zähler-Reset beim Neustart. Endpunkte und Hintergrundjobs werden **Bereichen**
   zugeordnet, jeder mit dem Worker-Typ, der ihn übernehmen würde (Sync → `synchrotron`,
   eingehende Föderation → `federation-inbound`, Medien → `media-repository`, …);
   Healthchecks und Verwaltung haben keinen.
2. **Sampler** (jede Minute, wie `nodehist`): alle Pods mit
   `k8s.element.io/synapse-instance`, direkt auf Port `synapse-metrics`. Gespeichert wird je
   Prozess und Minute: Kerne, Speicher, Kerne je Bereich, Warteschlangen. Tabelle
   `synapse_samples`, 30 Tage.
3. **Urteil** (reine Funktion, getestet): Python nutzt pro Prozess höchstens einen Kern.
   - zu wenig Messwerte → sagt das, mit seit wann;
   - Spitze (95. Perzentil, 7 Tage) unter 50 % → **kein Worker nötig**, mit der Zahl;
   - 50–80 % → beobachten, mit dem größten Bereich;
   - ab 80 % → **Worker empfohlen** für den größten zuordenbaren Bereich (≥ 25 % der Last);
     ist keiner so groß, sagt es das statt zu raten;
   - Föderations-Warteschlange dauerhaft lang → `federation-sender`, unabhängig von der CPU;
   - ein eingeschalteter Worker, der fast nichts tut → Hinweis, was er an Speicher kostet.
4. **Seite „Worker-Insights"**: Urteil; Prozesse (CPU jetzt/Schnitt/Spitze, Speicher,
   Neustarts, eingeschaltet vs. läuft); wohin die Last des Hauptprozesses geht; Verlauf
   24 h / 7 Tage.
5. **Einschalten über die Einstellungen**, nicht über einen zweiten Speicherweg: neue
   Aufgaben-Karte „Synapse-Worker" (Schalter je sinnvollem Typ, mit Speicherkosten im
   Hilfetext). Die Vorschau zeigt dann Neustarts und ob der Speicher auf dem Knoten reicht.
   Die Einstellungen bekommen `?card=…`, damit die Seite direkt dorthin führt.

## Fertig wenn

- Tests: Parser gegen eine echte Synapse-Ausgabe (gekürzt); Raten mit Zähler-Reset; jede
  Urteils-Regel mit Gegenprobe; Zuordnung Endpunkt → Bereich.
- Live: Sampler auf der Spielwiese; Urteil für Produktion „kein Worker nötig" mit der
  gemessenen Zahl.
