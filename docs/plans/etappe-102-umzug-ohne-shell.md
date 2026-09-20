# Etappen 102–105 — Ein Umzug, für den man keine Shell braucht

## Auslöser

> „es soll aber über die webui funktionieren like apple user like unifi user like daus"

Der Operator hat das Migrationsarchiv, das ich ihm von Hand gebaut habe, in das
Wiederherstellen-Feld gezogen und `kein Manifest im Archiv` bekommen. Er hat recht:
ein Umzug, für den man `pg_restore` in einer SSH-Sitzung tippt, ist kein Umzug für
Menschen. Und ein Produkt, dessen Antwort auf „sichere alles" ein README mit acht
Shell-Befehlen ist, hat die Aufgabe nicht gelöst, sondern weitergereicht.

## Was das Archiv heute kann — nachgesehen

`CreateFull` schreibt zwei Teile:

| Teil | Inhalt | wiederherstellbar über die UI |
|---|---|---|
| `matrixctrl/` | Konfigurations-Repo mit Historie, MatrixCtrls Datenbank | **ja** |
| `homeserver/` | Synapses Datenbank | **nein** — der Restore überspringt sie ausdrücklich |

Nicht enthalten, und das sind genau die Teile, die einen Umzug ausmachen:

- **Die MAS-Datenbank.** Dort liegen die **Konten**. `exportHomeserverUnder` exportiert
  nur `synapse`.
- **Die Medien.** Als `NotIncluded` benannt: das Volume bindet nur der Synapse-Pod ein.
- **Die Schlüssel** (`ess-generated`). Ohne `SYNAPSE_MACAROON` ist jede Sitzung
  ungültig, ohne `MAS_ENCRYPTION_SECRET` ist die wiederhergestellte Konten-Datenbank
  **nicht entschlüsselbar**. Ein Backup ohne sie sieht vollständig aus und ist es nicht.

Die ehrliche Zusammenfassung: das Archiv kann MatrixCtrl sichern und den Homeserver
nur *mitnehmen*, nicht zurückbringen.

## Zielzustand

Ein Knopf „Vollständiges Archiv" und ein Knopf „Wiederherstellen", die miteinander
reden. Was das eine schreibt, liest das andere zurück — ohne Shell.

## Die vier harten Stellen, keine davon geraten

1. **Die Medien liegen auf einem Volume, das nur Synapse einbindet.** MatrixCtrl kommt
   über die Exec-Subresource von client-go heran und streamt ein tar aus dem Pod. Das
   ist kein `exec("kubectl")` (Regel 4), sondern dieselbe API, über die der Pod-Log
   gelesen wird — aber es ist eine neue Fähigkeit und gehört benannt.
2. **Die Schlüssel in ein herunterladbares Archiv zu legen, ist eine
   Sicherheitsentscheidung.** Wer das Archiv hat, hat den Homeserver: Signaturschlüssel,
   Macaroon, MAS-Verschlüsselung. Das darf nicht beiläufig passieren — Vorschlag: ein
   eigenes Häkchen mit ausgeschriebener Konsequenz, standardmäßig aus, und das Archiv
   nennt im Dateinamen, dass es Schlüssel enthält.
3. **Das Zurückspielen ist destruktiv und dauert.** Synapse und MAS anhalten, zwei
   Datenbanken ersetzen, Medien entpacken, Schlüssel setzen, hochfahren. Es braucht
   denselben Fortschritts-Stream wie ein Upgrade und dieselbe Regel wie §4.88: **erst
   umschalten, wenn es funktioniert** — bei einem Fehlschlag muss der alte Zustand
   stehen bleiben, nicht die Hälfte des neuen.
4. **Die Größe.** Hier sind es 85 MB, anderswo Gigabyte. Durch Cloudflares kostenlosen
   Tarif passen 100 MB, und der Browser hält den ganzen Upload im Speicher. Das ist die
   Stelle, an der „über die WebUI" an eine Grenze stößt, die nicht in diesem Programm
   liegt.

## Etappen

### E102 — Das Archiv wird vollständig

`CreateFull` bekommt drei Teile dazu: `mas/` (die Konten), `media/` (aus dem Pod
gestreamt) und optional `secrets/`. Das Manifest zählt auf, was drin ist **und was
fehlt**, wie bisher — nur stimmt die Liste dann.

*Fertig wenn* ein Archiv dieselben Zahlen trägt, die ich von Hand gemessen habe:
4 Konten, 9 Räume, 19 064 Events, 675 Mediendateien.

### E103 — Zurückspielen, was drin ist

Der Restore erkennt die Teile am Manifest und stellt sie her: Datenbanken über die
vorhandenen Verbindungen, Medien in den Pod, Schlüssel als Secret. Mit Live-Log,
Abbruch bei Fehlschlag und der Prüfung danach („19 064 Events erwartet, 19 064
gefunden").

*Fertig wenn* ein leerer Server allein aus einem Archiv in den Zustand des alten kommt
und man sich **ohne neue Anmeldung** einloggen kann.

### E104 — Von Server zu Server, ohne Datei

Der bessere Weg als Herunterladen-und-Hochladen, und der, den ein Apple- oder
UniFi-Nutzer erwartet: man trägt kein Gerät zum anderen, man zeigt das neue auf das
alte. Hier sogar umgekehrt, weil es zur Lage passt:

**Der alte Server schiebt.** Auf ihm „Diesen Server umziehen" → Adresse des neuen und
ein Zugangs-Token eingeben → er streamt alles direkt hinüber. Kein Browser dazwischen,
keine Datei, keine 100-MB-Grenze, kein doppelter Transport.

Das trifft genau die Topologie dieser Installation: der alte Node hat **nur private
Adressen** und ist von außen nicht erreichbar — aber ausgehend kommt er raus, gemessen.
Eine Richtung, die funktioniert, ist besser als zwei, die es nicht tun.

Bleibt der Datei-Weg für alle, die keine zwei laufenden Server gleichzeitig haben. Dort
gilt: die Grenze davor muss **benannt** werden, bevor jemand zwanzig Minuten hochlädt und
dann ein 413 bekommt (seit v0.1.88 sagt die Meldung wenigstens, wer abgelehnt hat).

### E105 — Der Umzug als ein Vorgang

Auf dem neuen Server: Archiv hochladen → es steht drin, welcher Server das war, welche
ESS-Version, wie viele Konten → deployen und alles einspielen, in einem Ablauf. Das ist
Etappe 82, nur mit dem vollständigen Archiv dahinter.

## Entschieden (Operator, 2026-09-20)

1. **Schlüssel ins Archiv: ja, standardmäßig an** — und der empfindliche Teil
   verschlüsselt, mit einem automatisch erzeugten Schlüssel.
2. **Medien: aktivierbar**, nicht erzwungen.
3. **Pod-Exec: ja.**

### Wo der Wiederherstellungsschlüssel liegt, entscheidet alles

Ein erzeugter Schlüssel nützt nur, wenn er **nicht** mitreist und **nicht** auf dem
Server bleibt:

| Aufbewahrung | Folge |
|---|---|
| im Archiv | keine Verschlüsselung, nur Verschleierung |
| im Cluster | wer den Server hat, hat das Archiv — und auf einem **neuen** Server fehlt er, also scheitert genau der Umzug, für den das Archiv da ist |
| **beim Operator** | das Archiv ist unterwegs und im Cloud-Speicher sicher, und der Umzug funktioniert |

Also: pro Archiv ein Schlüssel, **einmal** beim Herunterladen angezeigt, nirgends
gespeichert. Wie ein Wiederherstellungsschlüssel bei Element oder Apple — was auch der
Erwartung entspricht, die „like apple user" ausdrückt.

**Und der Verlust darf nicht alles kosten.** Nur der `secrets/`-Teil wird verschlüsselt.
Wer den Schlüssel verliert, bekommt Konten, Räume, Nachrichten und Medien trotzdem
zurück — nur die Sitzungen brechen, weil das Macaroon fehlt. Ein Archiv, das ohne
Schlüssel **ganz** wertlos wäre, wäre die schlechtere Konstruktion.

## Weiterhin offen

1. **Schlüssel ins Archiv?** Ohne sie ist kein 1:1-Umzug möglich. Mit ihnen ist die
   Datei ein Generalschlüssel. Vorschlag: Häkchen, standardmäßig aus, Warnung im
   Klartext, Hinweis im Dateinamen.
2. **Medien ins Archiv?** Hier 38 MB, anderswo hunderte GB. Vorschlag: eigenes Häkchen
   mit der gemessenen Größe daneben, damit die Entscheidung mit einer Zahl getroffen
   wird und nicht mit einem Gefühl.
3. **Pod-Exec als neue Fähigkeit** — einverstanden? Es ist der einzige Weg an das
   Medien-Volume, und es weitet aus, was MatrixCtrl im Cluster tut.
4. **Datei oder Direktübertragung zuerst?** E104 (Server zu Server) löst den konkreten
   Fall besser und ist mehr Arbeit; E102/E103 (vollständiges Archiv) nützen auch dem,
   der nur sichern will. Meine Empfehlung: 102 und 103 zuerst, weil die Direktübertragung
   ohnehin dieselben Teile transportiert — sie braucht sie nur nicht als Datei.
