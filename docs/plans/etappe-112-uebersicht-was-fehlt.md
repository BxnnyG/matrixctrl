# Etappen 112–118 — Selbstbericht „was fehlt" und Reihenfolge

Stand 2026-09-29. Anlass, Betreiber: „weiter mit den Tabs TLS & DNS, Föderation, Bridges,
Worker-Insights · Versionen von allem auf den ersten Blick aufs Dashboard · QoL, Selbst-
bericht was fehlt — Einstellungen sind teils noch unübersichtlich, auch für Leute, die null
Ahnung haben · Updates sind noch langsam".

## Selbstbericht (angesehen, nicht erschlossen — lokale Instanz, Playwright)

| Wo | Befund | Für wen ein Problem |
|---|---|---|
| Einstellungen → Synapse | Oben stehen `Additional`, `Appservices`, `Extra Args`, `Extra Init Containers` — Helm-Innereien, alphabetisch, englisch. Was man sucht (Registrierung, Upload-Größe, Föderation, E-Mail) steht nirgends als solches | Einsteiger: findet nichts; Profi: scrollt |
| Einstellungen → Navigation | Abschnitte heißen wie Chart-Schlüssel („Matrix Authenticatio…", „Well Known Delegation") | Einsteiger |
| Navigation | TLS & DNS, Föderation, Bridges, Worker-Insights sind ausgegraute Einträge ohne Seite; ein grauer Eintrag neben einer fertigen Funktion wirkt kaputt (§ E70) | alle |
| Dashboard | Zeigt Gesundheit, aber **keine einzige Version**: nicht die von ESS, nicht Synapse/MAS/Element, nicht ob es Updates gibt | alle |
| Updates | ~1 min pro Übernehmen bleibt: das Chart startet bei jedem Deploy seine Hook-Jobs (init-secrets, check-config, deployment-markers) neu und zieht deren Images (`imagePullPolicy: Always` bei Tags). Nicht abschaltbar, aber **erklärbar**; die 10-Minuten-Hänger waren Fehler (festgeschriebene Images), seit 0.1.102 vorher gesperrt | alle |
| Alter Server | läuft noch MatrixCtrl 0.1.100 und hat am 29.09. denselben matrix-tools-Fehler produziert, den 0.1.102 verhindert | Betreiber |

Die Einstellungs-Befunde decken sich mit dem Konzept „Aufgaben-Ebene als Startansicht"
aus Plan 107 (Betreiber-Entscheidung 1, 2026-09-27) — es ist noch nicht gebaut.

## Reihenfolge

| Etappe | Inhalt | Warum hier |
|---|---|---|
| **112 — Versionen auf einen Blick** | Dashboard-Karte: MatrixCtrl (+ Update), ESS-Chart (+ neueste), je Dienst die laufende Version aus dem Image | klein, sofort sichtbar, alle Bausteine da |
| **113 — Aufgaben I** | Aufgaben-Ebene als Startansicht der Einstellungen: Server & Adressen, Registrierung & Anmeldung (verlinkt die Seite „Anmeldung"), Ressourcen & Kapazität — deutsch, ~15 Einstellungen, jede mit „was passiert beim Übernehmen" | größter Hebel für „null Ahnung" |
| **114 — Aufgaben II** | Nachrichten & Medien, Föderation (Einstellungen), Anrufe, Aussehen, E-Mail | |
| **115 — TLS & DNS** | je Hostname: DNS (A/AAAA, Cloudflare-Proxy ja/nein), Zertifikat von außen und am Ursprung, Ablauf; erklärt die Fälle vom 26.–28.09. (grau → selbstsigniert, orange → Cluster-intern Timeout) | Grundlage stand im Setup (E80) |
| **116 — Föderation** | Delegation prüfen (`.well-known/matrix/server`), Erreichbarkeit von außen, Liste der föderierten Server mit Fehlern/Backoff (Synapse-Admin-API `federation/destinations`) | |
| **117 — Worker-Insights** | welche Synapse-Worker das Chart kennt, welche laufen, Last je Worker | Recherche: ESS-Worker-Werte |
| **118 — Bridges** | Hookshot (im Chart) + registrierte Appservices; was ESS nicht mitbringt, wird gesagt, nicht vorgetäuscht | Recherche nötig |

Ausgegraute Navigation: Einträge verschwinden, bis ihre Seite existiert (statt grau).
