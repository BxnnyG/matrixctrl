# Etappe 114 — Aufgaben II

Stand 2026-09-29. Fortsetzung von 113, gleicher Mechanismus (`internal/tasks`).

## Nachgelesen im Chart 26.9.3

- Upload-Größe ist ein Chart-Wert: `synapse.media.maxUploadSize` (Standard `100M`,
  Synapse-Format mit M/K).
- URL-Vorschauen sind im Chart **an**, samt Sperrliste interner Adressen (Underrides) —
  ausschalten ist gefahrlos, einschalten braucht keine eigene Liste.
- Element Web liest `elementWeb.additional.<name>` als **JSON-Text** (ohne `.config`); MAS
  und Synapse als YAML unter `.config`. `internal/tasks` bekommt eine zweite Block-Form.

## Karten

| Karte | Felder |
|---|---|
| Nachrichten & Medien | Upload-Größe; Link-Vorschauen; wie lange Medien anderer Server zwischengespeichert werden; wie lange gelöschte Nachrichten noch für Moderation liegen |
| Föderation | mit wem der Server spricht: alle / keine / nur diese Liste (`federation_domain_whitelist`); öffentliches Raumverzeichnis für andere Server |
| Anrufe | Element Call an/aus; direkte Netzwerkanbindung der SFU (`hostNetwork`); TURN; Ports (mit Hinweis Firewall) |
| Aussehen | App-Name in Element Web (`brand`); Standard-Farbschema |

Neue Feldarten: Größe (100M), Dauer (7d), Port, Auswahl, Server-Liste.

## 114b (eigene Etappe) — E-Mail

SMTP braucht ein Passwort; es gehört wie die Client-Secrets der Anbieter in ein
Kubernetes-Secret, nicht in den Block. Eigener Mechanismus wie §4.112.

## Fertig wenn

- Element-Web-Felder schreiben gültiges JSON in `elementWeb.additional.matrixctrl-tasks`
  und lesen fremde JSON-Blöcke (Test mit Gegenprobe).
- „Keine Föderation" schreibt eine leere Liste, „alle" entfernt den Schlüssel (Test).
- Ports außerhalb 1–65535, Größen ohne Einheit, Dauern ohne Einheit werden abgelehnt.
