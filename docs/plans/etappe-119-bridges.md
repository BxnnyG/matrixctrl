# Etappe 119 — Bridges

Stand 2026-10-11. Letzter ausgegrauter Menüpunkt. ROADMAP Phase 4: „Bridges — mautrix-* und
Hookshot hinter einer gemeinsamen Oberfläche".

## Befund (gelesen, ESS 26.10.0)

- **Hookshot ist Teil von ESS:** `hookshot.enabled` bringt die Bridge samt
  Appservice-Registrierung bei Synapse (das Chart verdrahtet beides), Cache in Valkey,
  Webhooks unter `https://<Synapse-Host>/_matrix/hookshot/webhook/…` (ohne eigenen Host),
  Bot `@hookshot:<Server>`, Rechte für alle Nutzer des Servers (`manageConnections`).
- **Aber:** Eingehende Webhooks brauchen `generic: enabled: true`, Feeds `feeds: enabled:
  true` — das Chart setzt beides nicht; die eigenen Tests von ESS tun es über einen
  `hookshot.additional`-Block. Ein Schalter `enabled` allein ergibt eine Bridge, die nichts
  annimmt.
- **Alle anderen Bridges** (mautrix-WhatsApp/-Telegram/-Signal …) betreibt ESS nicht. Es
  bindet sie über `synapse.appservices` an: eine Liste von Registrierungsdateien aus
  ConfigMaps/Secrets. Betreiben muss man sie selbst.

## Was gebaut wird (dieser Schritt)

1. **Einstellungs-Karte „Bridges"**: Hookshot an/aus; eingehende Webhooks; RSS-/Atom-Feeds
   — über die bestehende Aufgaben-Mechanik (Vorschau: was neu startet, ob der Speicher
   reicht). Die beiden Funktionen schreiben in den `hookshot.additional`-Block.
2. **Seite „Bridges"**:
   - Hookshot: eingeschaltet? läuft? Version; was an ist; **wie man es benutzt**, in
     Worten mit kopierbaren Befehlen (`@hookshot` einladen, `!hookshot webhook <name>`,
     `!hookshot feed <url>`), samt der Adresse, unter der Webhooks ankommen.
   - Andere Bridges: jede Registrierung aus `synapse.appservices` mit Name, Adresse,
     Bot, Namensraum — **nie die Tokens** — und ob die Adresse aus dem Cluster erreichbar
     ist. Ohne Einträge: ehrlich sagen, dass MatrixCtrl solche Bridges nicht betreibt,
     nur anmeldet, und wie das geht.

## Danach (eigener Schritt)

- „Bridge anmelden": Registrierung einfügen → geprüft → als Secret im ESS-Namespace →
  in `synapse.appservices` → Übernehmen. Und wieder abmelden.

## Fertig wenn

- Tests: Registrierung lesen ohne Tokens; Erreichbarkeit; die Karte schreibt
  `hookshot.enabled` und die beiden Funktionen in den eigenen Block — je mit Gegenprobe.
- Live: Hookshot auf der Spielwiese einschalten, Webhook anlegen, ein POST darauf kommt als
  Nachricht im Raum an.
