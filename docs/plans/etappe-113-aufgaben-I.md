# Etappe 113 — Aufgaben I: Einstellungen für Einsteiger

Stand 2026-09-29. Konzept: Plan 107 („Ebene 1 — Aufgaben"), Betreiber-Entscheidung
2026-09-27: **die Aufgaben-Ebene wird die Startansicht**. Befund: Plan 112 (Synapse zeigt
oben `Additional`, `Appservices`, `Extra Args` …).

## Was gebaut wird

1. **`internal/tasks`** — die Aufgaben-Felder als Daten, eine Stelle: deutscher Titel, eine
   Zeile Hilfe, Art (Schalter, Text, Menge), Quelle, Standard, „was beim Übernehmen
   passiert". Das Frontend rendert, was die API liefert; neue Felder brauchen kein
   Frontend.
2. **Zwei Quellen:**
   - *Helm-Werte* (`synapse.ingress.host`, `postgres.resources.requests.memory` …) —
     gelesen aus der zusammengeführten Konfiguration, geschrieben über
     `SetSectionValues` (kommentarerhaltend).
   - *Dienst-Konfiguration* (MAS `account.password_registration_enabled` …) — die gibt es
     nur über `additional.<key>.config`, einen YAML-Text. MatrixCtrl besitzt **einen**
     Block, `additional.matrixctrl-tasks`, und schreibt nur dort. Setzt ein anderer
     `additional`-Block denselben Schlüssel schon, wird das Feld **nur angezeigt**, mit
     dem Namen des Blocks — zwei Stellen für einen Wert und eine, die still gewinnt, wäre
     genau die Unübersichtlichkeit, die diese Etappe beseitigen soll.
3. **Drei Karten:**
   - **Server & Adressen:** Servername (gesperrt, mit Erklärung), die Adresse jedes
     Dienstes.
   - **Registrierung & Anmeldung:** Registrierung mit Passwort, E-Mail verlangen,
     Passwort-Login, Selbst-Löschen; Verweis auf die Seite „Anmeldung" (externe Anbieter).
     Passwort-Login aus warnt: erst den Admin-Account mit einem Anbieter verknüpfen.
   - **Ressourcen & Kapazität:** Speicher und CPU (reserviert / höchstens) für Synapse,
     Datenbank, Anmeldung — mit Balken: was alle Dienste zusammen reservieren, gegen den
     Knoten, **nach** den ausstehenden Änderungen (aus der Vorschau).
4. **Startansicht:** Aufgaben · Alle Einstellungen · YAML · Änderungen. Änderungen landen
   im Arbeitsstand wie jede andere — die Leiste „Änderungen ausstehend" übernimmt.
5. **Vorschau** liefert zusätzlich die Reservierungen je Dienst und die Knotengröße
   (`capacity.Requests`), damit der Balken dieselbe Rechnung zeigt wie die Sperre.

## Nicht in 113 (→ 114)

Nachrichten & Medien, Föderation, Anrufe, Aussehen, E-Mail. DNS-/Zertifikatsprüfung je
Adresse → 115 (TLS & DNS).

## Fertig wenn

- Die Einstellungen öffnen auf „Aufgaben"; drei Karten, deutsch.
- Ein Schalter schreibt nur `additional.matrixctrl-tasks` und lässt alle anderen Blöcke und
  Kommentare unberührt (Test).
- Ein Schlüssel, den ein anderer Block setzt, ist gesperrt und nennt den Block (Test mit
  Gegenprobe).
- „Zurück auf Standard" entfernt den Wert, statt den Standard hineinzuschreiben.
- Der Kapazitätsbalken zeigt nach einer Speicheränderung die neue Summe, bevor
  übernommen wird.
