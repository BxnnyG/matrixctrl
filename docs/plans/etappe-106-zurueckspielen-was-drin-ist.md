# Etappe 106 — Zurückspielen, was drin ist

> „backup und wiederherstellen mit allen daten chats verläufe calls accounts, und dann
> 1 zu 1 mich wieder einloggen per elemnt also mas"
> „es soll aber über die webui funktionieren like apple user like unifi user like daus"

Seit Etappe 102 ist das Archiv vollständig: beide Datenbanken, die Medien, die
versiegelten Schlüssel. Der Restore schreibt weiterhin **nur MatrixCtrls eigenen
Teil** zurück. Das Manifest sagt das ehrlich — aber ehrlich sein über eine Lücke ist
nicht dasselbe wie sie zu schließen.

## Was gemessen wurde, bevor irgendetwas geplant wurde

Am laufenden Cluster, nicht aus dem Code gelesen:

```
synapse                   173 Tabellen, 25 Sequenzen, 0 Views, Erweiterungen: plpgsql
matrixauthenticationservice 34 Tabellen,  0 Sequenzen
Eigentümer aller Tabellen und Sequenzen: synapse_user
Rollen: postgres (super, createdb) · synapse_user (weder) · mas_user (weder)
```

Und die Zahl, die den Plan umschreibt:

```
device_lists_sequence         = 47485
cache_invalidation_stream_seq = 33066
account_data_sequence         =  1590
… 25 Sequenzen, davon 4 nie benutzt (last_value NULL)
```

**Im Archiv stehen null davon.** Der Export listet `pg_class.relkind = 'r'` — gewöhnliche
Tabellen. Sequenzen sind `relkind = 'S'` und fallen durch.

## Warum „Zeilen zurückschreiben" ein kaputter Server ist

Eine Sequenz ist kein Datensatz, sondern ein Zähler, und Synapse zieht daraus die
`stream_ordering` jedes Events, die `state_group`-IDs, die Geräte-Listen-Positionen.
Spielt man die Zeilen zurück und lässt die Zähler bei 1 stehen, vergibt Synapse Nummern,
die in den wiederhergestellten Zeilen bereits vergeben sind:

| Folge | wie es sich zeigt |
|---|---|
| Kollision auf einem Primärschlüssel | Synapse schreibt nicht mehr — sichtbar, immerhin |
| neue Events sortieren sich **vor** die ganze Historie | Sync liefert nichts Neues, Räume wirken eingefroren |
| `state_group_id` doppelt vergeben | zwei Räume teilen sich einen Zustand — stille Beschädigung |

Das Dritte ist das Schlimme: es sieht nach einem geglückten Umzug aus. Ein Restore, der
das tut, ist schlechter als keiner, weil er das Vertrauen einsammelt, bevor der Schaden
sichtbar wird.

Daraus folgt die Reihenfolge dieser Etappe: **erst das Archiv reparieren, dann
zurückspielen.** Ein Archiv ohne Sequenzen ist nicht zu 1:1 wiederherstellbar, und die
Archive, die es schon gibt — auch das 85-MB-Archiv des Operators — sind es nicht. Das
muss die Oberfläche sagen, nicht das Manifest verschweigen.

## Die Entscheidung, die hier umgedreht wird

Im Archiv steht seit Etappe 70 ein Satz, der bei jedem Homeserver-Export mitgeliefert
wird:

> „Zurückspielen ist bewusst kein Knopf: dafür muss Synapse gestoppt sein, und es im
> laufenden Betrieb zu tun beschädigt, was da ist."

Der Satz war richtig — als Begründung dafür, es **nicht** zu bauen. Er wird jetzt zur
Anforderungsliste, denn beide Gründe sind adressierbar:

1. *„Synapse muss gestoppt sein"* — MatrixCtrl darf StatefulSets skalieren. Das ist ein
   **Schritt**, kein Hindernis.
2. *„im laufenden Betrieb beschädigt es, was da ist"* — stimmt, wenn man in die
   laufende Datenbank schreibt. Also wird nicht hineingeschrieben (siehe unten).

Was bleibt: es ist der gefährlichste Knopf im Produkt. Er bekommt deshalb die
Eigenschaft, die ihn erträglich macht — **bei einem Fehlschlag steht der alte Zustand
noch** (§4.88).

## Der Kern: nebenan aufbauen, dann umschalten

Nicht `TRUNCATE` + `COPY` in die laufende Datenbank. Sondern:

```
1. synapse_restore_<ts> anlegen           ← die alte Datenbank wird nicht angefasst
2. Schema erzeugen, Daten laden, Sequenzen setzen, Zeilen zählen
3. Synapse + MAS auf 0 skalieren          ← ab hier Ausfall, Sekunden
4. ALTER DATABASE synapse RENAME TO synapse_vor_<ts>
   ALTER DATABASE synapse_restore_<ts> RENAME TO synapse
5. Medien-Verzeichnis im Pod tauschen, Secret anwenden
6. hochskalieren, prüfen
```

Scheitert Schritt 1–2, ist nichts passiert: die Wiederherstellung liegt in einer
Datenbank, die niemand benutzt, und wird weggeworfen. Scheitert Schritt 4 auf halbem
Weg, sind beide Namen bekannt und die Rückumbenennung ist ein Befehl — der in der
Fehlermeldung steht, nicht in der Dokumentation.

Die alte Datenbank wird **nicht** gelöscht. Sie heißt `synapse_vor_<ts>` und braucht
Platz; genau deshalb nennt die Oberfläche vorher die freie Kapazität und hinterher den
Befehl, mit dem man sie los wird. Etwas, das man nach einer geglückten Migration selbst
wegräumt, ist besser als etwas, das man nach einer misslungenen nicht mehr hat.

**Das braucht die `postgres`-Rolle**, nicht `synapse_user`: `CREATE DATABASE` und
`ALTER DATABASE … RENAME` kann nur ein Superuser, und `synapse_user` hat weder
`createdb` noch `super` (gemessen). Das Passwort liegt in `ess-generated`
(`POSTGRES_ADMIN_PASSWORD`), das MatrixCtrl ohnehin liest. Es wird pro Vorgang gelesen
und nicht gehalten — wie `synapseDSN` es schon tut.

### Woher das Schema kommt

Das Archiv trägt **keins** (§4.66), und das soll so bleiben. Für MatrixCtrls eigene
Datenbank entsteht es aus den Migrationen. Für Synapse und MAS entsteht es, indem der
jeweilige Dienst einmal gegen die leere Datenbank startet — er legt sein Schema selbst
an, in der Version, die gerade läuft. Also:

```
leere DB anlegen → Dienst kurz dagegen starten → Schema da → Dienst stoppen → Daten laden
```

Das ist umständlicher als ein `pg_dump -s` im Archiv und dafür richtig, wenn das Ziel
eine **neuere** ESS-Version fährt als die Quelle: das Schema ist dann das der neuen, und
Synapse migriert die geladenen Daten beim nächsten Start selbst. Ein mitgeliefertes
Schema würde den Zielserver auf den Stand der Quelle zurückziehen.

## Was gebaut wird

1. **Archivformat 2: Sequenzen.** `pg_sequences` je Datenbank-Teil, mit `last_value` und
   `is_called`; ein nie benutzter Zähler wird als „nicht aufgerufen" gespeichert, nicht
   als 0 — das ist ein Unterschied, den `setval` kennt. Ein Live-Test liest sie und
   besteht nur, wenn die benutzten Zähler **nicht** null sind: `last_value` ist für
   Fremde NULL statt Fehler, und `synapse_user` ist zwar Eigentümer, aber das ist eine
   Messung und keine Annahme (§4.105).
2. **Format 1 wird gelesen, aber ehrlich beschriftet.** Bestehende Archive bleiben
   einspielbar — und der Vorschau-Schirm sagt, dass die Zähler fehlen und was das
   heißt. Ein stiller Erfolg wäre hier der teuerste Fehler des Produkts.
3. **`RestoreAll`**, teilweise auswählbar: Konfiguration · Synapse · Konten · Medien ·
   Schlüssel. Jeder Teil einzeln abwählbar, jeder mit seinem eigenen Ergebnis.
4. **Der Schattendatenbank-Ablauf** aus dem Abschnitt oben, mit Live-Protokoll über den
   bestehenden WebSocket-Kanal (wie der Upgrade-Strom, E14) statt einer Anfrage, die
   zwanzig Minuten offen steht.
5. **Medien in den Pod**, über exec mit Stdin (das Gegenstück zu `TarFromPod`, E102) in
   ein Staging-Verzeichnis, danach Verzeichnistausch. Kein Entpacken über die
   Anwendung: der Strom geht durch, wie er beim Sichern durchging.
6. **Die Schlüssel** als Secret, nach Eingabe des Wiederherstellungsschlüssels. Ohne ihn
   läuft der Rest trotzdem — der Teil ist absichtlich der einzige verschlüsselte
   (§4.102).
7. **Die Prüfung danach**, aus dem Manifest gegen die neue Datenbank: „19 064 Events
   erwartet, 19 064 gefunden", je Tabelle, und ein Vergleich der Sequenzen. Ein Restore,
   der nur „fertig" sagt, ist ein Restore, der nichts über sein Ergebnis weiß.

## Randfälle (die acht aus PROZESS)

1. **Kein ESS / ESS woanders:** ohne Release gibt es nichts zu stoppen und keine
   Datenbank — der Ablauf bricht *vor* dem ersten Schreibvorgang ab und sagt, dass erst
   deployt werden muss (das ist Etappe 108).
2. **Release in schlechtem Zustand:** `pending-*` oder `failed` → gar nicht erst
   anfangen. Ein Restore, der mitten in einen halben Upgrade fährt, ist nicht
   zurückzunehmen.
3. **Nicht nur Deployments:** Synapse und Postgres sind StatefulSets, MAS ist ein
   Deployment. Das Herunterskalieren muss beide Arten kennen, sonst läuft Synapse weiter
   und schreibt in die Datenbank, die gerade umbenannt wird.
4. **Cluster langsam oder weg:** jeder Schritt mit Zeitgrenze; ein Abbruch zwischen 3
   und 6 hinterlässt den Zustand „abgeschaltet, alte Datenbank unter altem Namen" — der
   ist wiederherstellbar und wird in der Meldung benannt.
5. **Kein Internet:** nichts hier braucht welches.
6. **Beide Auth-Modi:** Restore ist Admin-only; im Bootstrap-Modus erreichbar, weil ein
   frischer Server genau dort steht.
7. **Config-Randformen:** unverändert, der Teil funktioniert seit E73.
8. **Helm ok, Hooks fehlgeschlagen:** nach dem Hochskalieren laufen die Hooks erneut —
   das ist der bestehende Weg und wird nicht umgangen.

Dazu drei eigene:

- **Zu wenig Platz** für eine zweite Kopie der Datenbank → vorher messen und benennen,
  nicht beim `COPY` auffliegen.
- **Fremder Server im Archiv:** `serverName` der Quelle ≠ Ziel. Das ist kein Umzug,
  sondern eine Übernahme — nur mit ausdrücklicher Bestätigung, weil die Signierschlüssel
  dann zu einem anderen Namen gehören.
- **Ältere ESS-Version im Archiv als auf dem Ziel:** erlaubt, siehe Schema-Abschnitt.
  Umgekehrt (Archiv neuer als Ziel) nicht: dann fehlen Spalten, die die Daten brauchen.

## Bewusst nicht

- **Kein Server-zu-Server** — das ist Etappe 107 und der bessere Weg für große
  Installationen; hier geht es um die Datei, die es schon gibt.
- **Kein Deployen aus dem Archiv** (ESS-Version lesen und installieren) — Etappe 108.
- **Kein Teil-Restore einzelner Räume oder Konten.** Ein Archiv ist ein Zeitpunkt.

## Fertig wenn

- Ein Archiv aus diesem Cluster spielt in eine **leere** Datenbank zurück, und danach
  stimmen Tabellenzahlen **und** alle 25 Sequenzen mit dem Manifest überein.
- Der Umzugsfall ist echt geprüft: nach dem Zurückspielen meldet sich ein bestehendes
  Konto **ohne neue Anmeldung** an (das ist der Zweck des `secrets/`-Teils).
- Ein absichtlich mitten im Ablauf abgebrochener Restore hinterlässt die alte Datenbank
  unter ihrem Namen und einen Server, der wieder hochkommt.
- Ein Format-1-Archiv wird angenommen **und** sagt vorher, was fehlt.
- `make check` grün, die vier S11-Prüfungen durch — im Cluster gefragt, nicht über die
  öffentlichen Namen, die seit dem DNS-Umzug auf einen anderen Server zeigen (§4.105).
