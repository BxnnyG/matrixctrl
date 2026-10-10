# Etappe 119b — Hooks ablösen

Stand 2026-10-10. *Nachgetragen: Dieser Plan wurde parallel zum Code geschrieben statt
davor — ein Verstoß gegen PROZESS §1, hier offen vermerkt.*

Betreiber-Frage: „die Auto-Skripte, Hooks heißen die — braucht man die, sind die noch
zeitgemäß, sollten sie für jeden automatisch an sein, verstehen das Laien?"

## Befund (gelesen)

- Zwei eingebaute Hooks, beide für den Anruf-Server: `hostNetwork` (+ `dnsPolicy:
  ClusterFirstWithHostNet`) am SFU-Deployment, `externalTrafficPolicy: Local` an drei
  SFU-Services. Gebaut, als das ESS-Chart das nicht konnte und jedes Upgrade Anrufe brach.
- ESS 26.5+ kann beides als normale Einstellung (`matrixRTC.sfu.hostNetwork`,
  `…exposedServices.*.externalTrafficPolicy`); bei `hostNetwork` rendert das Chart die
  `dnsPolicy` selbst.
- Produktion: beide Werte stehen in den Helm-Werten, das Manifest enthält alles, was die
  Hooks patchen. Die Hooks patchen Gleiches mit Gleichem und rollen dafür den SFU neu.
- Neuinstallationen über MatrixCtrl schreiben diese Werte **nicht** — dort machen die
  Hooks Anrufe bisher erst möglich.

## Was gebaut wird

1. **Abdeckung prüfen, allgemein:** Für jeden Patch eines Hooks die gesetzten Pfade
   ableiten (JSON-Patch add/replace, Merge-Patch-Blätter) und gegen das gerenderte
   Release-Manifest vergleichen (nicht gegen das Live-Objekt — das trägt den Patch ja
   selbst). Alles abgedeckt → der Lauf wird als „übersprungen" mit Begründung
   protokolliert. Ein Hook mit HTTP-Aufruf, `remove`-Patch oder ohne Manifest läuft
   immer.
2. **Neuinstallationen** schreiben die Anruf-Werte direkt; gegen das Chart-Schema
   getestet (die wellKnownDelegation-Lektion).
3. **Oberfläche:** „Nicht mehr nötig" mit Grund am Hook; die Zusammenfassung zählt, was
   wirklich etwas zu tun hat; Erklärung in Worten. Hooks wandern ins Menü „Erweitert".

## Fertig wenn

- Tests aus den echten eingebauten Hooks: beide abgedeckt; halb abgedeckt (ohne
  `dnsPolicy`) läuft; ein Service auf `Cluster` läuft; HTTP-Mischung läuft — je mit
  Gegenprobe.
- Live gegen das Produktions-Manifest: beide „nicht mehr nötig".
