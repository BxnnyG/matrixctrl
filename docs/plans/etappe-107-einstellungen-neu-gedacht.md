# Etappe 107–111 — Einstellungen, neu gedacht

> „die ist sau unübersichtlich, man weiß nicht, was man wo einstellt, nicht intuitiv …
> aus DAU-Sicht, aus Apple-, UniFi-, Grafana-, IT-ler-Sicht"

Der Operator hat gerade einen kompletten Server umgezogen und dabei jede Stelle der
Konfiguration gebraucht. Das ist der ehrlichste Test, den diese Seite je bekommen hat,
und sie hat ihn nicht bestanden. Dieser Plan beginnt deshalb mit einem Selbstbericht —
**angesehen, nicht aus dem Code erschlossen**: eine lokale Instanz mit einer Kopie der
echten Konfiguration, ohne Cluster-Zugriff, von Playwright fotografiert.

## Selbstbericht

### Die Zahlen

| | |
|---|---|
| Einstellungen im Formular | **1 411** |
| davon „nur via YAML" (im Formular nicht bedienbar) | **331** |
| größter Abschnitt (Synapse) | 550 Felder, bis zu 4 Ebenen tief |
| Einstellungen, die ein Operator realistisch anfasst | etwa 25–30 |
| mitgelieferte Schema-Version | 26.5.x — **die laufende ESS ist 26.8.0** |

Die Seite zeigt das Helm-Chart. Sie zeigt nicht, was man damit tun will.

### Was konkret falsch ist

**1. Gegliedert nach Dateien, nicht nach Aufgaben.** Die Navigation listet
`general.yaml`, `synapse.yaml`, `matrixAuthenticationService.yaml` … mit Feldzählern
(„550"). Niemand denkt „ich will in `synapse.yaml`". Man denkt „wer darf sich
registrieren?", „warum gehen Anrufe nicht?", „Login mit Google".

**2. Was man wirklich will, ist gar kein Feld.** Registrierung, Upload-Größe,
Föderation, Aufbewahrung (Synapse) und externe Anmelde-Anbieter (MAS) liegen in den
Freitext-Blöcken `additional` — im Formular „nur via YAML". Bei Synapse sind die ersten
sechs Einträge nach „Enabled" ausnahmslos solche Sackgassen: `Additional`,
`Appservices`, `Extra Args`, `Extra Init Containers`, `Extra Volume Mounts`,
`Host Aliases`.

**3. Die Einstellung, die gerade einen Server lahmgelegt hat, ist unsichtbar.**
Speicher und CPU (`resources.requests/limits`) sind im Schema Freiform-Maps. Das
Formular zeigt sie als „nur via YAML", und die Suche nach „memory" findet nur zwei
Redis-Felder — nicht Postgres, nicht Synapse. Beim Umzug forderte Postgres 8,6 Gi auf
einem 8-Gi-Knoten an und blieb fünf Tage `Pending`; in der Konsole war das weder zu
sehen noch zu finden noch zu ändern.

**4. Gefährliches sieht harmlos aus.** Das allererste Feld der Seite ist `serverName`
als gewöhnliches Textfeld — mit dem Hinweis darunter, dass es sich nach dem ersten
Deploy nicht ändern lässt. Ein Tippfehler und ein Deploy zerstören alle Nutzer- und
Raum-IDs.

**5. Das Schema passt nicht zur laufenden Version.** Mitgeliefert ist nur `26.5.x`,
deployed ist `26.8.0`. Felder aus 26.6–26.8 fehlen, entfernte werden angeboten, und
validiert wird gegen das falsche Chart.

**6. Die Hilfetexte sind kaputt.** Die Kommentar-Zuordnung klebt die Doku
auskommentierter Schlüssel an das nächste echte Feld: Die Beschreibung von „Additional"
bei MAS ist ein Block aus PostgreSQL-Port, Benutzer, Datenbank, sslMode, Passwort und
zwei Secrets. YAML-Beispiele werden zu Fließtext plattgedrückt. In den Abschnittsdateien
selbst ist die Einrückung auskommentierter Blöcke verrutscht (ein `# postgres:`-Block
steht innerhalb von `checkConfigHook.resources.limits`).

**7. Chart-Vokabular, halb Englisch.** „Tls Enabled" (die Humanisierung kennt keine
Abkürzungen), „Class Name", „Controller Type", „Host Aliases" — englische Chart-Doku in
einer deutschen Oberfläche.

**8. „Speichern" und „Deployen" sind Git und Helm, nicht Nutzersprache.** Speichern
committet, auf dem Server ändert sich nichts. Wer „Speichern" drückt, glaubt, es sei
erledigt. **Im YAML-Modus gibt es gar keinen Deploy-Knopf** — man muss den Tab wechseln.

**9. Zwei YAML-Editoren, der bessere ist verwaist.** `/config/$slice` kann Validierung,
Commit-Nachricht, Diff und Deploy. Nichts verlinkt darauf. Der erreichbare YAML-Modus
kann nichts davon — die neunte Wiederholung von „gebaut, nie angeschlossen" (§4.79).

**10. Drei Eingänge zum selben Verlauf.** „Versionen & Diff" in der App-Navigation,
„Verlauf" im Seitenkopf, der Tab „Änderungen".

**11. Kein Zustand sichtbar.** Leere Felder zeigen ihren Standardwert nicht. Nirgends
steht, was geändert, was ausstehend, was live ist, was einen Neustart auslöst, was nicht
auf den Knoten passt. Das Deploy zeigt ein rohes Helm-Log.

**12. Enge.** Zwei Seitenleisten übereinander (App und Abschnitte) nehmen ein Drittel
der Breite; Namen werden abgeschnitten („Matrix Authenticatio…"). Ein Entwickler-Detail
(`/data/config-repo · 5 Versionen`) steht in der Haupt-Werkzeugleiste.

**Was gut ist und bleibt:** das Designsystem (Tokens, Karten, Dark-Theme), die
Git-Versionierung, das kommentarerhaltende Schreiben (S11 #2), die Suche als Idee.

## Durch fünf Brillen

| Wer | will | bekommt heute |
|---|---|---|
| **DAU** | „Läuft alles? Wer darf rein? Name ändern." In seiner Sprache, ohne YAML, mit Rückgängig. | 1 411 englische Chart-Felder und zwei Knöpfe, deren Unterschied Git-Wissen voraussetzt |
| **Apple** | wenige, gut gewählte Entscheidungen; jede mit klarer Folge; Gefährliches hinter Bestätigung; es funktioniert einfach | jede Option gleichwertig nebeneinander, `serverName` als offenes Textfeld |
| **UniFi** | Karten pro Komponente mit Status; „Erweitert" klappt Expertenfelder auf; eine Leiste „Änderungen ausstehend → Übernehmen"; sichtbares Provisioning | Dateien statt Komponenten, kein Zustand, rohes Helm-Log |
| **Grafana** | alles durchsuchbar (⌘K), jede Einstellung verlinkbar, JSON/YAML-Modell immer einen Klick entfernt und synchron, Speichern mit Diff und Nachricht, Verlauf mit Wiederherstellen | Suche ohne verschachtelte Schlüssel, zwei Editoren, drei Verlaufs-Eingänge |
| **IT-ler / SRE** | Wahrheit: was läuft vs. was konfiguriert ist; Diff und Dry-Run vor dem Anwenden; Kapazität; Rollback; Validierung gegen die *laufende* Chart-Version; roher Zugriff jederzeit | falsches Schema, keine Kapazitätsprüfung im Formular, keine Drift-Anzeige hier |

Keine der fünf Gruppen will dieselbe Seite — aber alle fünf wollen **Schichten**. Der
DAU soll die Expertenebene nie sehen müssen; der SRE soll nie durch die DAU-Ebene müssen.

## Das Konzept: drei Ebenen, eine Änderungs-Pipeline

### Ebene 1 — Aufgaben (Standard, für 90 % der Besuche)

Handgeschrieben, deutsch, nach Aufgaben gegliedert. Rund 30 Einstellungen, die
tatsächlich angefasst werden — der Rest bleibt Chart-Standard.

| Karte | enthält | vorhandener Baustein |
|---|---|---|
| **Server & Adressen** | Servername 🔒 (nur lesbar, mit Erklärung); Domain je Dienst mit **Live-Prüfung**: zeigt der Name auf diesen Server, ist das Zertifikat gültig, oder landet er bei Cloudflare auf etwas anderem | `internal/dnscheck`, DNS-Prüfung aus dem Setup (E80) |
| **Anmeldung & Registrierung** | Registrierung offen / nur mit Einladung / zu; **Anmeldung mit externen Anbietern** (Google, GitHub, Apple, Keycloak/Authentik, generisches OIDC) als geführter Assistent: Anbieter wählen → Client-ID/Secret → die Redirect-URL zum Kopieren wird angezeigt → „Testen"; Passwort-Login an/aus | MAS-Konfiguration (`additional`) |
| **Nachrichten & Medien** | max. Upload-Größe (mit Einheit), Aufbewahrung, URL-Vorschauen | Synapse `additional` |
| **Föderation** | an/aus, Erlaubt-/Sperrliste, Erreichbarkeit von außen | Föderations-Delegation |
| **Anrufe** | Element Call an/aus, RTC-Adresse, Ports, **Erreichbarkeitstest**, TURN | RTC-Seite, Reachability (E19, E26) |
| **Aussehen** | Markenname in Element, Standard-Theme, Willkommensseite | Element-Web-Konfiguration |
| **Ressourcen & Kapazität** | Speicher/CPU je Dienst mit **Kapazitätsbalken**: „Knoten 7,9 Gi · angefordert 4,7 Gi · nach dieser Änderung 5,6 Gi ✓ passt" — oder ✗ mit dem Grund | Kapazitäts-Vorprüfung (E55, E56), `WhyUnschedulable` |
| **E-Mail** | SMTP für Passwort-Reset und Verifizierung | MAS-Konfiguration |

Jede Einstellung trägt: Klartext-Titel, eine Zeile Erklärung, das Bedienelement, einen
**Zustand** (Standard · geändert · ausstehend · live) und „was beim Übernehmen passiert"
(„Synapse startet neu, ca. 30 s").

### Ebene 2 — Alle Einstellungen (Experte)

Das vollständige Schema, aber:

- **Schema der laufenden Chart-Version**, über das Helm-SDK aus dem deployten Chart
  gelesen, statt eines mitgelieferten 26.5.x.
- Pro Komponente die fachlichen Felder oben; Kubernetes-Innereien (securityContext,
  Probes, tolerations, labels, annotations, `extra*`, image, serviceAccount) unter
  „Kubernetes-Details", standardmäßig zu.
- **Maps und Listen bedienbar** (Schlüssel-Wert-Editor für `resources`, labels;
  Listeneditor für Arrays) statt „nur via YAML".
- Reparierte Hilfetexte; YAML-Beispiele als Codeblöcke.
- Suche, die verschachtelte Schlüssel findet und deutsche Synonyme kennt
  („Speicher" → memory, „Arbeitsspeicher", „Upload").

### Ebene 3 — YAML (IT-ler)

**Ein** Editor — der bessere aus `/config/$slice`, hierher verlegt: Validierung gegen das
laufende Schema, Fehler inline, Diff daneben, Commit-Nachricht, und derselbe
Übernehmen-Weg wie die anderen Ebenen. Formular und YAML sind dieselben Daten.

### Quer durch: eine Änderungs-Pipeline

Statt „Speichern" und „Deployen" eine Leiste am unteren Rand, sobald etwas geändert ist:

```
3 Änderungen ausstehend · Synapse startet neu (~30 s)    [Ansehen] [Verwerfen] [Übernehmen]
```

„Übernehmen" heißt: speichern → Dry-Run gegen den Cluster → Kapazitätsprüfung →
anwenden → Fortschritt als Komponenten-Liste („Postgres ✓ · Synapse startet… · MAS ✓")
statt Helm-Log (das bleibt aufklappbar) → bei Fehlschlag Rücksprung auf den letzten
guten Stand anbieten (§4.91). „Als Entwurf speichern" wird ausdrücklich zweitrangig.
Ein Verlauf, ein Eingang, mit Wiederherstellen.

### Leitplanken

- `serverName` gesperrt; ändern nur mit Eintippen des Namens und ausgeschriebener Folge.
- Einstellungen mit Neustart oder Ausfall sind markiert.
- Kapazitätsprüfung **vor** dem Anwenden — was nicht auf den Knoten passt, wird nicht
  angewendet, sondern erklärt.
- Domain-Einstellungen prüfen live DNS und Zertifikat.
- Drift-Hinweis, wenn der Cluster vom Konfigurierten abweicht (E21, E25).

## Etappen

| Etappe | Inhalt | Risiko |
|---|---|---|
| **107 — Wahrheit** | Schema aus dem laufenden Chart; Kommentar-Zuordnung reparieren; Abkürzungen (TLS, URL, DNS, CPU, ID); Maps editierbar (zuerst `resources`); verschachtelte Suche; YAML-Modus bekommt Validierung und Deploy, `/config/$slice` wird aufgelöst; ein Verlaufs-Eingang | klein — behebt, was falsch ist |
| **108 — Übernehmen** | Leiste „Änderungen ausstehend"; ein Knopf mit Dry-Run, Kapazitätsprüfung, Neustart-Vorhersage, Fortschritt, Rücksprung-Angebot | mittel — ersetzt den Deploy-Weg der Seite |
| **109 — Aufgaben I** | Server & Adressen (mit Live-DNS/TLS), Ressourcen & Kapazität, **Anmeldung inkl. externer Anbieter** | mittel |
| **110 — Aufgaben II** | Nachrichten & Medien, Föderation, Anrufe, Aussehen, E-Mail | mittel |
| **111 — Expertenebene** | Kubernetes-Details gruppiert, Listeneditoren, Synonym-Suche, ⌘K-Direktlinks auf jede Einstellung | klein |

107 kommt zuerst, weil jede spätere Ebene auf einem Schema aufsetzt, das zur laufenden
Version passt, und auf Hilfetexten, die stimmen. Die externen Anbieter aus 109 sind der
akute Wunsch des Operators und können vorgezogen werden, sobald 107 steht.

Die bisher geplanten 107–108 (Umzug als ein Vorgang, Server-zu-Server) rücken auf
112–113: Der konkrete Umzug ist erledigt, der Bedarf ist nicht mehr akut.

## Bewusst nicht

- **Kein neues Designsystem.** Tokens, Karten und `mc.tsx` bleiben; es geht um die
  Gliederung, nicht um die Farbe.
- **Keine Abschaffung von YAML.** Die Expertenebene und der Editor bleiben vollwertig.
- **Keine automatische Übersetzung der Chart-Doku.** Ebene 1 wird deutsch geschrieben;
  Ebene 2 zeigt die Chart-Texte, repariert, im Original.

## Entschieden (Operator, 2026-09-27)

1. **Die Aufgaben-Ebene wird die Startansicht**, „Alle Einstellungen" und YAML sind
   Umschalter daneben.
2. **Anbieter zuerst: generisches OIDC, Google, GitHub.**
3. **„Übernehmen" fragt bei einem Fehlschlag**, mit vorausgewähltem Rücksprung auf den
   letzten guten Stand.

## Fertig wenn

- Ein Nutzer ohne Kubernetes-Wissen ändert die Upload-Grenze, schaltet die Registrierung
  zu und richtet „Login mit Google" ein, **ohne YAML zu sehen**.
- Die Suche nach „Speicher" und nach „memory" findet die Ressourcen von Postgres und
  Synapse.
- Eine Änderung, die nicht auf den Knoten passt, wird **vor** dem Anwenden abgelehnt,
  mit dem Grund.
- `serverName` lässt sich nicht versehentlich ändern.
- Ein falsch gerouteter Domain-Name (zeigt auf etwas anderes als diesen Server) ist in
  „Server & Adressen" rot markiert — der Fehler, der beim Umzug den Login blockiert hat.
- Genau ein YAML-Editor, genau ein Verlaufs-Eingang.

---

## Etappe 108 — Übernehmen (Umsetzungsplan, 2026-09-28)

### Was gemessen wurde

Die Bausteine gibt es alle — sie sind nur nicht zu einem Weg verbunden:

| Baustein | Stand |
|---|---|
| Manifest rendern (`helm.Render`) | vorhanden (E55) |
| Kapazität prüfen (`capacity.Check`) | vorhanden — **aber nur als Warnung**, erst *nach* dem Commit, mitten im laufenden Deploy (P1-16c) |
| Rollback | vorhanden (API, Knopf auf der Update-Seite) |
| Verwerfen | fehlt — der Store kann Commit und Diff, `git.ResetToCommit` gibt es |
| Fortschritt pro Dienst | der Stream liefert ihn (`onProgress`), die Config-Seite zeigt nur Rohlog |

**P1-16c wird entschieden.** Der Eintrag sagte: scharf schalten, „sobald die Prüfung in
Produktion ein paar Mal recht hatte". Sie hatte zweimal recht, beide Male mit echtem
Ausfall — August (Postgres 35 h unschedulebar) und September (Postgres 5 Tage `Pending`
nach dem Umzug). Beide Male stand die Warnung im Log, und das Deploy lief trotzdem.

### Was gebaut wird

1. **`POST /api/v1/config/preview`** — die Frage „was würde Übernehmen tun?", ohne etwas
   zu tun. Rendert die ausstehende Konfiguration, prüft die Kapazität, und vergleicht die
   Pod-Vorlagen des gerenderten Manifests mit denen des laufenden Releases: **welche
   Dienste starten neu**. Kein Commit, kein Apply.
2. **Die Kapazitätsprüfung sperrt** — vor dem Commit, synchron, als `409` mit den
   Befunden. Ein `override_capacity: true` bleibt als bewusster Ausweg für den Fall, dass
   die Prüfung irrt; die Oberfläche versteckt ihn hinter einer ausdrücklichen Bestätigung.
3. **`POST /api/v1/config/discard`** — Arbeitsstand auf den letzten Commit zurück.
4. **Die Leiste „Änderungen ausstehend"** am unteren Rand der Einstellungen, sobald der
   Arbeitsstand vom letzten Commit abweicht: Anzahl, vorhergesagte Neustarts,
   `[Ansehen] [Verwerfen] [Übernehmen]`. „Übernehmen" fragt zuerst die Vorschau; sperrt
   sie, steht der Grund da statt eines Deploys.
5. **Fortschritt als Dienst-Liste** statt Rohlog (das bleibt aufklappbar) — eine geteilte
   Komponente, die die Update-Seite ebenfalls nutzen kann.
6. **Bei Fehlschlag fragen, mit vorausgewähltem Rücksprung** (Entscheidung 3):
   „Auf den letzten guten Stand zurück" ist der vorausgewählte Knopf, daneben „So lassen".

### Randfälle

- **Kein Cluster / Render scheitert:** die Vorschau sagt „konnte nicht geprüft werden" —
  und das sperrt nicht (unbekannt ≠ passt nicht), wird aber sichtbar genannt (§4.55).
- **Release in schlechtem Zustand** (`pending-*`, `failed`): Übernehmen verweist auf den
  Rücksprung, statt in einen halben Zustand zu deployen (§4.88).
- **Keine ausstehenden Änderungen:** keine Leiste; „Deployen" des aktuellen Stands bleibt
  über den Seitenkopf möglich (z. B. um Hooks neu anzuwenden).
- **Beide Auth-Modi:** alles hinter derselben Admin-Prüfung wie der bisherige Deploy.

### Fertig wenn

- Eine Änderung, die Postgres auf 4 Gi setzt, wird auf einem 8-Gi-Knoten **vor** dem
  Anwenden abgelehnt, mit Dienst, Anforderung und freiem Platz im Satz — und nichts wurde
  committet.
- Die Vorschau nennt die Dienste, die neu starten, bevor irgendetwas passiert.
- „Verwerfen" setzt den Arbeitsstand zurück, und die Leiste verschwindet.
- Ein fehlgeschlagenes Übernehmen bietet den Rücksprung vorausgewählt an.

### Nachtrag beim Bauen: was „Rücksprung" zurücksetzt

Der vorhandene Rollback-Knopf setzt nur den Cluster zurück, mit `revision: 0` („die
vorige"). Für den Fehlschlag nach „Übernehmen" reicht das nicht, zweimal:

- **Die Konfiguration bliebe auf dem kaputten Stand.** Übernehmen committet vor dem
  Deploy. Rollt nur Helm zurück, sagt das Repository etwas anderes als der Cluster, und
  das nächste Übernehmen rollt denselben Fehler wieder aus.
- **„Die vorige" ist nicht immer die richtige.** Scheitert das Upgrade, bevor Helm eine
  Revision schreibt (Render-Fehler, gesperrtes Release), ist „die vorige" eine zu weit.

Darum merkt sich jedes Übernehmen, *wovon* es ausging — Commit und Helm-Revision — und
`POST /api/v1/config/revert-apply {upgrade_id}` geht genau dorthin: Cluster auf die
gemerkte Revision (nur wenn sich die Revision überhaupt geändert hat, mit den
post-rollback-Hooks wie beim bestehenden Rollback), danach die Konfiguration als **neuer
Commit** mit dem Inhalt von damals. Kein Hard-Reset: der Verlauf behält, was versucht
wurde und dass es zurückgenommen wurde.

Der Merkzettel lebt im Stream, also im Speicher. Nach einem Neustart von MatrixCtrl ist
er weg — dann sagt der Endpunkt das und verweist auf Verlauf und Update-Seite, statt zu
raten.
