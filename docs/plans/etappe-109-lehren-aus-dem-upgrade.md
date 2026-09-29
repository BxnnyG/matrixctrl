# Etappe 109 — Lehren aus dem Upgrade auf 26.9.3

Stand 2026-09-29. Eingeschoben vor „Aufgaben I" (jetzt 110). Anlass: das erste Upgrade
auf dem neuen Server, 2026-09-28, sechs Revisionen bis zu einem laufenden Stand.

## Was passiert ist

| # | Symptom | Ursache | Wer hat es gefunden |
|---|---|---|---|
| 1 | `ess-init-secrets` Exit 1, Revision 4 `failed` | `matrixTools` in der Config auf 0.7.3 festgeschrieben; das Chart 26.9.3 verlangt einen Secret-Typ, den erst 0.10 kennt | Log des Jobs, von Hand |
| 2 | Valkey nie bereit, SFU im CrashLoop, Revision 5 `failed` | `redis.image` auf `library/redis:7.4` festgeschrieben; 26.9 ersetzt Redis durch Valkey und prüft mit `valkey-cli` | Pod-Events, von Hand |
| 3 | Übernehmen scheitert nach 1 s — und bietet den Rücksprung an | Schema-Fehler in der Vorprüfung, **vor** Helm. Der Rücksprung hätte die gerade eingetragene Korrektur zurückgenommen | Betreiber, zum Glück nicht geklickt |
| 4 | Grund des Abbruchs nirgends im Server-Log | Stream-Zeilen gingen nur an den Browser-Tab | beim Nachsehen |
| 5 | Element Call: `OPEN_ID_ERROR … Unable to create room on SFU` | `matrixRTC.hostAliases` zeigte auf die Traefik-ClusterIP **des alten Clusters** — beim Umzug ungeprüft mitgekommen. Der Auth-Dienst 0.7 legt Räume über diese Adresse an, 0.4 tat das nicht | Vergleich mit dem alten Cluster, von Hand |
| 6 | „Fertig" dreht weiter, obwohl fertig | Stepper kannte keinen Endzustand | Betreiber |
| 7 | MatrixCtrl speichert seine Verlaufswerte nicht mehr | `BIGSERIAL`-Zähler beim Umzug nicht nachgezogen → `duplicate key` jede Minute | im Log gesehen |

Muster: **Jeder dieser Fehler war vorher sichtbar oder ableitbar** — im Chart, in der
Config, im alten Cluster —, und keiner wurde vorher gesagt.

## Was gebaut wird

1. **Stepper** (6): „Fertig" wird abgehakt; ein Schritt, auf dem es scheiterte, zeigt ✗
   statt Spinner. Geteilt, also auch auf der Update-Seite.
2. **Rücksprung nur, wenn etwas angewendet wurde** (3): die Oberfläche liest die
   Release-Revision vor dem Übernehmen und nach einem Fehlschlag. Gleich → „auf dem
   Cluster hat sich nichts geändert", kein Rücksprung-Angebot. Nicht lesbar → Angebot wie
   bisher (unbekannt ≠ unverändert). Nicht an der Phase entschieden: die Vorprüfung, die
   hier ablehnte, läuft in „Anwenden".
3. **Fehlerzeilen ins Server-Log** (4): jede `ERROR`/`WARNING`-Zeile eines Streams.
4. **Zähler nachziehen beim Start** (7): nach den Migrationen jede `serial`/`identity`-
   Sequenz der eigenen Tabellen auf `max(id)`, nur vorwärts. Idempotent; kostet einen
   Durchlauf über wenige Tabellen.
5. **Verwaiste Cluster-IPs** (5): die Vorschau des Übernehmens meldet jede IP aus
   `10.43.0.0/16`-artigen Service-Netzen in `hostAliases`, zu der es im Cluster keinen
   Service gibt — mit dem Service, den es unter diesem Namen jetzt gibt, falls eindeutig
   (Traefik). Warnung, nicht Sperre: eine hostAlias-IP kann bewusst extern sein.
6. **Festgeschriebene Image-Versionen** (1, 2): **offene Entscheidung, siehe unten.**

## Offene Entscheidung (Betreiber)

E31 hat entschieden: ältere festgeschriebene Tags werden gemeldet, aber weder gesperrt
noch automatisch gelöst — ein Tag zu lösen kann eine große Versionssprung-Migration
auslösen, und das entscheidet der Betreiber. Gestern hat genau diese Warnung zweimal
ein Upgrade scheitern lassen. Vorschlag: **vor** dem Start auf der Update-Seite zeigen,
mit einem Knopf „dem Chart folgen lassen" (die Zeilen werden auskommentiert, nicht
gelöscht), und das Upgrade sperren, solange Tags älter als das Ziel-Chart sind — mit
bewusstem Übergehen wie bei der Kapazität.

## Fertig wenn

- Nach einem erfolgreichen Übernehmen ist „Fertig" abgehakt; nach einem gescheiterten
  steht ✗ auf dem Schritt, auf dem es scheiterte.
- Ein Übernehmen, das an der Schema-Prüfung scheitert, bietet keinen Rücksprung an.
- Der Grund eines gescheiterten Übernehmens steht im Server-Log.
- Nach einem Neustart auf dem neuen Server verschwinden die `duplicate key`-Zeilen.
- Die Vorschau hätte `10.43.222.87` gemeldet (Test mit genau diesem Fall).
