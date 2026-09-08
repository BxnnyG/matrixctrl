# Etappe 95 — Was tatsächlich veröffentlicht wurde

## Auslöser

Der Rückstand wurde durchgesehen. Die meisten offenen Punkte sagen selbst, dass sie
gerade nicht dran sind — und das ist richtig:

| Punkt | Warum nicht jetzt |
|---|---|
| P1-14 (TURN-Relay) | Vorbedingung P1-13 ist ungemessen: nichts Eingehendes erreicht den Node. Ein Relay hinter derselben unbewiesenen Portfreigabe wäre das vierte „müsste jetzt gehen" |
| P2-19 (Audit-Log) | Nachgemessen: 22 Zeilen / 64 kB in einem Monat. Die Begründung stimmt, die Dringlichkeit war falsch |
| P1-10, P1-12, P2-20 | Der Produktteil ist längst gebaut (E22, E24, E19); die Einträge stehen für die Lehre |

Übrig bleibt **P2-7**, und der ist fällig: „Ursache entfernt 2026-09-03 (E66)".

## Befund

Der Release-Workflow baut seit E66 `linux/amd64,linux/arm64` und schiebt beides als
ein Manifest. Nachgesehen an `0.1.81`:

    mediaType: application/vnd.oci.image.index.v1+json
      linux/amd64   sha256:6f847bd0cb824
      linux/arm64   sha256:b98c6de82cc8c

Und eine Ebene tiefer, im Config-Blob des arm64-Manifests:

    architecture: arm64 | os: linux
    entrypoint: ['/usr/local/bin/matrixctrl']
    3 layers, 23.0 MB

Das ist ein echtes Image, kein leerer Eintrag im Index.

**Das README sagt trotzdem weiter**, arm64 sei „untested" — geschrieben, als noch kein
getaggter Release eines produziert hatte. Inzwischen haben es zwanzig getan.

## Der eigentliche Punkt

Geprüft hat das bis eben **niemand**. Der Workflow schiebt und ist zufrieden; ob im
Register danach steht, was er zu schieben glaubte, hat nie jemand nachgesehen — bis
0.1.70 gar nicht erst veröffentlicht wurde und ich es erst bemerkte, als der Operator
mit der alten Version dastand (§4.75).

Dieselbe Sache, eine Ebene weiter: damals fehlte die Veröffentlichung ganz, hier könnte
eine Architektur fehlen und niemand würde es merken — bis jemand mit einem ARM-Board
`helm install` ausführt und ein `no matching manifest` bekommt.

**Der Tag ist die Absicht, die Registry ist das Ergebnis.** Also fragt der Release nach
dem Schieben die Registry.

## Was gebaut wird

- `scripts/check-published.sh <version>` — fragt GHCR: existiert der Chart-Tag, ist das
  Image ein Manifest-Index, sind `linux/amd64` **und** `linux/arm64` darin, und meldet
  jedes Manifest mit Architektur und Größe. Läuft ohne Anmeldung, mit dem anonymen
  Pull-Token, das jeder Client bekommt.
- Als letzter Schritt im Release-Workflow, nach dem Push. Scheitert er, ist die
  Veröffentlichung rot — und zwar mit dem Grund, nicht mit einem Schweigen.
- README: die arm64-Aussage auf das korrigieren, was belegt ist. **Nicht** auf
  „unterstützt": veröffentlicht und strukturell geprüft ist nicht dasselbe wie auf
  echter Hardware gelaufen, und diese Unterscheidung ist der Grund, warum der alte Satz
  überhaupt dastand.
- BACKLOG: P2-7 mit dem Beleg schließen, die Einschränkung benennen.

## Gegenprobe

Das Skript gegen eine Version laufen lassen, die es gibt (0.1.81 — muss durchgehen),
und gegen eine, die es nicht gibt (muss scheitern und sagen warum).
