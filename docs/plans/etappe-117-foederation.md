# Etappe 117 — Föderation

Stand 2026-09-30. Der Menüpunkt steht seit Phase 1 ausgegraut da. Zwei Fragen, die ein
Betreiber zur Föderation hat, und heute nirgends beantwortet bekommt:

1. **Erreichen mich andere Server?** Wenn nicht, sieht man es nur daran, dass Einladungen
   von außen nie ankommen — und niemand sagt, woran es liegt.
2. **Mit wem redet mein Server, und mit wem klappt es gerade nicht?**

## 1. Erreichbarkeit — nachgegangen wie ein fremder Server

Der Weg, den die Spezifikation (Server-Server-API, „Resolving server names") vorgibt,
Schritt für Schritt, jeder mit Ergebnis und Grund:

1. **Wegweiser** `https://<server_name>/.well-known/matrix/server` → `m.server`.
   Fehlt er: kein Fehler an sich, aber dann versuchen andere `<server_name>:8448` —
   hinter Cloudflare wird 8448 nicht durchgereicht, also der häufigste Grund für
   „Föderation geht nicht". Liefert er HTML statt JSON (Catch-all einer Webseite): Fehler.
2. **Ziel bestimmen:** aus dem Wegweiser (mit Port → direkt; ohne → SRV
   `_matrix-fed._tcp`, sonst 8448), ohne Wegweiser SRV auf den Server-Namen, sonst 8448.
   Gesagt wird, *woher* das Ziel kommt.
3. **Verbinden und Schlüssel holen:** TLS zum Ziel, das Zertifikat muss für den
   delegierten Namen gelten (nicht für das SRV-Ziel); `GET /_matrix/key/v2/server` muss
   den eigenen Server-Namen nennen — ein Wegweiser, der auf einen anderen Homeserver
   zeigt, fällt hier auf.
4. **Version:** `GET /_matrix/federation/v1/version` — welche Software antwortet.
5. **Client-Wegweiser** `/.well-known/matrix/client` → `m.homeserver.base_url`, mit
   `Access-Control-Allow-Origin` (ohne findet Element Web den Server nicht).

Ehrlich beschriftet: geprüft **von diesem Server aus über das öffentliche Netz**, nicht
von außen. Für den Blick von außen ein Link auf den Federation Tester von matrix.org —
ein Klick des Betreibers, MatrixCtrl selbst schickt nichts dorthin.

Eigenes Paket `internal/federation`, der Netzwerkzugriff injizierbar (Resolver, Dialer,
Vertrauensanker), damit die Tests echte TLS-Verbindungen gegen einen Testserver machen.

## 2. Gegenstellen — aus Synapse

Synapse-Admin-API, mit dem Matrix-Zugriff des Betreibers wie bei Räumen:

- `GET /_synapse/admin/v1/federation/destinations` — alle Server, mit denen Synapse
  redet; `failure_ts` sagt „scheitert seit", `retry_last_ts` + `retry_interval` „nächster
  Versuch".
- `GET …/destinations/<d>/rooms` — welche gemeinsamen Räume daran hängen.
- `POST …/destinations/<d>/reset_connection` — „jetzt neu versuchen" statt bis zu Tagen
  Backoff abwarten (nur für scheiternde Ziele).

Seite: oben die Erreichbarkeit als Schrittliste mit Urteil, darunter die Gegenstellen,
scheiternde zuerst, Filter „nur Probleme", aufklappbar zu den Räumen.

## Nicht in dieser Etappe

- Föderation einschränken (Allow-/Denylist) — das ist eine Einstellung, keine Anzeige;
  eigene Aufgabe in den Einstellungen, wenn gewünscht.

## Fertig wenn

- Tests `internal/federation` gegen einen echten TLS-Testserver: gesunde Delegation;
  fehlender Wegweiser ohne 8448; HTML statt JSON; Schlüssel eines fremden Servers
  (Server-Name passt nicht); Zertifikat für den falschen Namen. Jeder mit Gegenprobe.
- Tests Synapse-Client: Ziele, Räume, Reset (Pfad-Escaping des Servernamens).
- Live: Erreichbarkeit des Produktions-Servers und der Spielwiese; Gegenstellen auf der
  Spielwiese.
