# Etappen 85–88 — Die Kette, die aussperrt

**Vorgezogen.** Die Etappen aus `etappe-85-sichtbarer-zustand.md` (Preflight, Rollback,
Zustandsseite, Aufräumen, Komponenten-Tests) rücken dahinter. Sie machen das Produkt
besser; die hier machen es benutzbar.

## Auslöser

Ein Operator hat den Server neu aufgesetzt. Setup lief gut, dann:

> „er sagt ey alles fertig jetzt mas verbinden obwohl das cluster nichtmal online war
> […] ERROR: helm upgrade: another operation (install/upgrade/rollback) is in progress
> […] dann wenn mas deployed ist und ich erneut auf setup gehe dann verbinden drücke
> passiert nada […] dann zu mas ich komme darauf aber nicht darein also no credencials
> gehen es kommen immer Invalid credentials"

Drei Sätze, vier Fehler, und zusammen ergeben sie eine **Aussperrung**.

## Die Kette

| | Was passiert | Warum |
|---|---|---|
| 1 | Setup zeigt „ESS fertig", während Helm noch installiert | `ess_installed` ist wahr, sobald eine Release **existiert** — auch in `pending-install`. `ess_status` wird sogar mitgeliefert und nirgends benutzt |
| 2 | „Verbinden" schreibt den Client, speichert OIDC, **dann** startet das Upgrade | `SaveOIDCConfig` steht vor dem `go func()` mit dem Upgrade (`helm_setup.go:188`) |
| 3 | Das Upgrade scheitert an der noch laufenden Operation | genau die Meldung aus dem Bericht |
| 4 | MatrixCtrl ist jetzt auf Matrix-Login umgestellt, MAS kennt den Client nicht | der lokale Login antwortet ab jetzt 403 (`auth.go:157`) |
| 5 | Erneut „Verbinden" tut nichts | `reconcileMASClient` antwortet 200 **ohne `upgrade_id`**; das Frontend startet einen Stream auf `undefined` |
| 6 | MAS sagt „Invalid credentials" | ein frisch ausgerolltes ESS hat **null Nutzer** |

Schritt 4 ist die Aussperrung: der lokale Weg ist zu, der neue existiert nicht, und
Schritt 5 verweigert die Reparatur, weil „registriert" an der **Konfigurationsdatei**
entschieden wird statt an MAS.

Nachgesehen: `internal/mas` kann sperren, entsperren, deaktivieren, löschen, zum Admin
machen, Passwort setzen — **nicht anlegen**. Die MAS-Admin-API kann es, geprüft an der
laufenden Instanz: `POST /api/admin/v1/users` („Create a new user").

## Sofort erledigt (Skript, ohne Release)

`install.sh recover-login` — stellt den lokalen Login wieder her. Erkennt beide Quellen
(Datenbank aus „Verbinden", Chart-Werte aus `oidc.enabled`), räumt die passende ab,
startet neu, nennt das Passwort. Rührt keinen Matrix-Account und keine ESS-Konfiguration
an.

**Und ein Fehler, den ich dabei selbst gebaut und ausgelöst habe.** `confirm` fiel ohne
Terminal auf die Vorgabe der Frage zurück. `recover-login` fragt mit Vorgabe „ja" — also
hat ein Testlauf ohne Terminal auf einer **produktiven Instanz** den Matrix-Login
abgeschaltet, ohne dass ein Mensch die Frage gesehen hat. Zurückgestellt, und die
Ursache behoben: **kein Terminal ist keine Zustimmung.** Eine Vorgabe gehört zu jemandem,
der schnell entscheidet, nicht zur Abwesenheit von jemandem.

## Etappen

### E85 — Kein „fertig", solange etwas läuft

- `ess_installed` heißt: Release **deployed** und Pods bereit. `pending-*` und `failed`
  sind eigene Zustände mit eigenem Text, nicht „installiert".
- Solange eine Helm-Operation läuft, ist der nächste Schritt gesperrt und sagt warum.
- Fertig wenn: „Verbinden" gar nicht erst erscheint, während installiert wird.

### E86 — Der erste Matrix-Nutzer

Ohne ihn ist der Umstieg auf Matrix-Login eine Sackgasse, und das ist der Grund für
„Invalid credentials".

- `internal/mas`: `CreateUser` neben dem vorhandenen `SetPassword`/`SetAdmin`.
- Im Setup ein eigener Schritt **vor** „Verbinden": Benutzername, Passwort, wird
  angelegt und zum Admin gemacht.
- Sind bereits Admins vorhanden, entfällt der Schritt und sagt, wer sie sind.
- Fertig wenn: nach einem Greenfield-Deploy ein Account existiert, mit dem man sich
  anmelden kann, bevor irgendetwas umgestellt wird.

### E87 — Umschalten erst, wenn es funktioniert

- `SaveOIDCConfig` nach hinten: erst nach erfolgreichem Upgrade **und** einer
  Discovery, die antwortet. Bis dahin bleibt der lokale Login offen.
- „Ist der Client registriert?" wird bei **MAS** erfragt, nicht in der Datei nachgesehen.
- Erneutes „Verbinden" führt den fehlenden Teil aus, statt 200 ohne Vorgang zu melden.
- Fertig wenn: ein gescheitertes Verbinden den Operator dort lässt, wo er war.

### E88 — Der Rückweg, sichtbar

`recover-login` gibt es im Skript. Die App muss den Zustand **benennen**: „Matrix-Login
ist eingerichtet, antwortet aber nicht" — mit dem Befehl daneben. Dazu ein Health-Check
auf den Issuer, statt ihn nur beim Start zu prüfen.

Der Anlass dafür ist heute entstanden: auf der produktiven Instanz löst der MAS-Name
inzwischen auf Cloudflare auf und liefert für `/.well-known/openid-configuration` ein
404. MAS selbst antwortet über den Cluster-Service einwandfrei. Die Anmeldung lief
weiter, weil OIDC beim **vorherigen** Start initialisiert worden war — erst ein Neustart
hat es sichtbar gemacht. Ein Zustand, der nur beim Start geprüft wird, ist kein Zustand,
sondern eine Erinnerung.

### E89 — Hostnames einzeln überschreibbar

> „schade ist das man die nicht da ändern kann sagen kann ey ich möchte es anstatt auf
> x.domain.tld auf y.domain.tld haben"

Die sechs Namen kommen aus `greenfieldHostnames`. Sie sollen einzeln editierbar sein,
mit der Ableitung als Vorgabe, und die DNS-Prüfung folgt der Änderung. Das Abhaken von
Hand („bissle spielerei") entfällt — die Prüfung weiß es besser.

## Danach

Die Etappen aus der Vorrunde, unverändert, nur später: Preflight vor Helm-Operationen ·
Rollback zur richtigen Zeit · Zustandsseite · Aufräumen · Komponenten-Tests.

**Self-Update ist entschieden und gestrichen**: der Operator will es als
`install.sh update`. Kein Job, kein Knopf.

## Weitere Szenarien, die dieselbe Klasse treffen

1. Doppelklick auf „Deployen", oder zwei offene Tabs → dieselbe „another operation"-Meldung.
2. Deploy erfolgreich, Pods nicht bereit (Image-Pull, zu wenig CPU) → „fertig" ist wieder falsch.
3. Ein Nutzer existiert, ist aber kein MAS-Admin → `requireAdmin` weist ab, ohne zu sagen warum.
4. DNS zeigt noch nicht hierher, wenn MAS neu startet → Discovery scheitert, Verbinden auch.
5. Der Operator zieht DNS auf einen neuen Server → der alte verliert die Anmeldung, ohne es zu merken (heute passiert).
6. Restore eines Archivs auf einen Server mit anderem ESS-Stand → die Plan-Ansicht aus E82 deckt das ab, der OIDC-Client darin aber nicht: der Client im Archiv gehört zum alten MAS.

Nummer 6 ist neu und gehört in E87: ein wiederhergestelltes Archiv bringt einen
OIDC-Client mit, den das frische MAS nicht kennt. „Registriert" an MAS zu prüfen statt
an der Datei löst auch das.
