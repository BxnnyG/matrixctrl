# Etappe 120 — Automatische Backups, außerhalb des Servers

Stand 2026-10-19. Heute gibt es Backups nur als Download aus dem Browser: wer es vergisst,
hat keins, und eins, das auf demselben Server liegt, stirbt mit ihm.

## Befund (gelesen)

- `backup.CreateFull` (`internal/backup/full.go:75`) schreibt ein tar.gz als Strom in einen
  `io.Writer`: Konfig-Repo samt Historie, MatrixCtrl-DB, Synapse-DB, MAS-DB, optional
  Medien (`TarFromPod`, Strom aus dem Synapse-Pod), optional versiegelte Geheimnisse.
  Gespeichert wird nichts; `BackupFull` (`internal/api/handlers/status.go`) streamt in die
  HTTP-Antwort und baut die Verbindungen (`synapseDSN`, `masDSN`) selbst auf.
- Versiegeln (`internal/backup/seal.go`): pro Download ein neuer symmetrischer Schlüssel,
  einmal per Header gezeigt. Für ein Backup, bei dem niemand zusieht, geht das nicht:
  den Schlüssel sähe niemand.
- Keine S3-Abhängigkeit, kein Zeitplan. Hintergrundjobs: Goroutine mit Ticker, gestartet in
  `cmd/matrixctrl/main.go` (Muster `synmetrics.Sampler.Run`).
- Geheimnisse: k8s-Secret wie SMTP (`internal/k8s/secrets.go`, `internal/mail`),
  Einstellungen in `instance_settings`.
- ESS-Upgrade: `HelmHandler.Upgrade` (`internal/api/handlers/helm_upgrade.go:24`), mit
  Vorab-Sperre für gepinnte Images (409 + `override_pinned_images`). Danach Goroutine mit
  Fortschritts-Stream bis `h.helm.Upgrade`.
- Restore (`restore_full.go`) nimmt nur einen Upload an und spoolt ihn in eine Temp-Datei.

## Entscheidungen des Betreibers (2026-10-19)

1. **Das ganze Archiv wird verschlüsselt**, mit einem öffentlichen Schlüssel. Das Panel
   erzeugt einmal ein Schlüsselpaar und zeigt den privaten Schlüssel ein einziges Mal an.
   Der Server behält nur den öffentlichen: Er kann verschlüsseln, aber nicht lesen. Beim
   S3-Anbieter liegt nichts lesbar.
2. **Vor jedem ESS-Upgrade (Versionswechsel) läuft ein Backup.** Schlägt es fehl, wird das
   Upgrade nicht gestartet; der Knopf „Ohne Backup fortfahren" lässt es trotzdem laufen.
3. **Medien:** Schalter, Standard an, gemessene Größe daneben. Das Backup vor einem Upgrade
   lässt die Medien immer weg, weil ein Upgrade sie nicht anfasst.
4. **Restore aus S3:** Das Panel listet die Backups im Bucket. Jedes kann heruntergeladen
   oder direkt wiederhergestellt werden, nachdem der private Schlüssel eingegeben wurde.

## Technische Wahl (Begründung)

- **Verschlüsselung: age** (`filippo.io/age`), X25519-Empfänger. Es verschlüsselt als Strom
  (passt zu `CreateFull`) und ist geprüft und klein. Der private Schlüssel ist eine Zeile
  `AGE-SECRET-KEY-1…`, ein Archiv lässt sich zur Not auch ohne MatrixCtrl mit dem
  `age`-Kommando öffnen. Wer schon ein eigenes Schlüsselpaar hat, kann stattdessen seinen
  öffentlichen Schlüssel (`age1…`) einfügen.
  Archive mit Ganz-Verschlüsselung tragen keinen eigenen Seal-Teil: Die Geheimnisse liegen
  unversiegelt im ohnehin verschlüsselten Archiv (ein Schlüssel statt zwei). Der Download
  aus dem Browser bleibt, wie er ist.
- **S3: `github.com/minio/minio-go/v7`.** Läuft mit jedem S3-kompatiblen Anbieter
  (Hetzner, Backblaze, Wasabi, MinIO, AWS) und lädt einen Strom unbekannter Länge in Teilen
  hoch. Es braucht nichts lokal zwischenzuspeichern und ist viel kleiner als das AWS-SDK.
- Strom: `CreateFull → age.Encrypt → io.Pipe → PutObject(size -1)`. Der Server braucht
  dafür keinen Platz. **Korrektur beim Bauen:** `CreateFull` legte die Medien bisher als
  *einen* Member `media/media.tar` ab und kopierte dafür den ganzen Strom erst in eine
  Temp-Datei, weil tar die Größe vorab braucht. Bei großen Medien wäre der Panel-Pod
  evicted worden, beim Browser-Download heute schon. Darum werden die Medien jetzt Datei
  für Datei als `media/files/…` übernommen (Archivformat 3). Alte Archive bleiben lesbar.
- Restore spoolt das Archiv weiterhin auf die Platte, auch aus S3: Ein Restore *mit*
  Medien braucht lokal Platz in Archivgröße. Das ist unverändert und bewusst nicht Teil
  dieser Etappe.
- S3-Zugang liegt im Secret `matrixctrl-backup-s3` im ESS-Namespace, weil die Role nur dort
  Secrets schreiben darf (wie beim Mail-Secret).
- Objektname: `<prefix>/matrixctrl-<yyyy-mm-dd-hhmm>-<anlass>.tar.gz.age`, Anlass
  `zeitplan` | `vor-upgrade-<version>` | `manuell`.

## Was gebaut wird

1. **`internal/backup/offsite`** (neu):
   - `Target`: Endpoint, Region, Bucket, Präfix, Path-Style; Zugangsschlüssel liegen im
     Secret `matrixctrl-backup-s3`, der Rest in `instance_settings`.
   - `Run(ctx, opts, anlass)`: Archiv bauen, verschlüsseln, hochladen. Fehlschläge werden
     *nicht* still geschluckt: Ein abgebrochener Upload wird abgebrochen
     (`AbortMultipartUpload`), damit keine halben Objekte liegen bleiben, die wie Backups
     aussehen.
   - `List`, `Open` (Strom aus S3), `Prune(policy)`.
   - Das Zusammensetzen der Teile (Verbindungen, Medien, Release) zieht aus `BackupFull`
     in eine gemeinsame Funktion. Download und Zeitplan bauen dasselbe Archiv
     (Zentralisierungs-Check).
2. **Aufbewahrung** als reine Funktion mit Tests: die letzten 7 täglichen und 4 wöchentlichen
   Backups behalten, und jedes vor-Upgrade-Backup 30 Tage. Gelöscht werden nur Objekte mit
   unserem Namensmuster im eigenen Präfix.
3. **Zeitplan:** ein Hintergrundjob (Ticker-Muster), täglich zur eingestellten Uhrzeit
   (Standard 03:30 UTC). Ist ein Backup gerade nicht möglich (Ziel nicht eingerichtet,
   anderes Backup läuft), wird nichts nachgeholt; der Zustand nennt den Grund. Den letzten
   Lauf (Zeit, Größe, Dauer, Fehler) speichert eine Tabelle `offsite_runs`.
4. **Vor dem Upgrade:** In `HelmHandler.Upgrade` läuft nach den Vorab-Sperren und vor
   `h.helm.Upgrade` ein Backup ohne Medien. Den Fortschritt zeigt der Upgrade-Stream.
   Schlägt es fehl, endet das Upgrade mit `failed` und dem Grund, ohne dass Helm etwas
   angefasst hat. Die Oberfläche bietet dann „Ohne Backup fortfahren"
   (`skip_backup: true`), und `upgrade_history` vermerkt das. Ist kein Ziel eingerichtet,
   schreibt der Stream einen Hinweis und das Upgrade läuft weiter. Es sperrt nur ein
   Backup, das eingerichtet ist und fehlschlägt.
5. **Restore aus S3:** `RestoreFull` bekommt eine zweite Quelle, den Objektnamen plus den
   privaten Schlüssel. Er wird nur für diese Anfrage benutzt und nie gespeichert. Danach
   geht es durch denselben Spool und denselben Ablauf wie ein Upload. `RestoreFull` lernt
   außerdem, `.age`-Uploads mit dem Schlüssel zu öffnen.
6. **Oberfläche (Backup-Seite):** Karte „Automatisch" mit Ziel (Endpoint, Bucket, Präfix,
   Zugang), Knopf „Verbindung prüfen" (Probe: Objekt schreiben, lesen, löschen),
   Schlüsselpaar erzeugen oder eigenen öffentlichen Schlüssel einfügen, Uhrzeit,
   Medien-Schalter mit Größe und „Jetzt sichern". Darunter die Liste der Backups im Bucket
   (Datum, Anlass, Größe) mit „Herunterladen" und „Wiederherstellen".

## Nicht in dieser Etappe

Mehrere Ziele · andere Ziele als S3 (SFTP, WebDAV) · inkrementelle Medien-Backups ·
Benachrichtigung bei Fehlschlag (Etappe „Benachrichtigungen", hängt sich dann an
`offsite_runs`) · Backup vor einem reinen Konfig-Apply.

## Fertig wenn

- Tests mit Gegenprobe:
  - Aufbewahrung: behalten und löschen an den Grenzen; fremde Objekte nie löschen.
  - Verschlüsselung: Rundreise; ein falscher Schlüssel muss scheitern; ein Archiv darf
    keinen Klartext-Manifest-String enthalten.
  - Ein abgebrochener Strom hinterlässt kein Objekt.
  - Ein Upgrade mit fehlschlagendem Backup ruft `helm.Upgrade` nicht auf, mit
    `skip_backup` schon.
- `make check` grün, die vier S11-Regressionsprüfungen bestanden.
- Live auf der Spielwiese gegen einen echten S3-kompatiblen Bucket: Zeitplan-Lauf, Liste,
  Restore aus dem Bucket in die Spielwiese, ein ESS-Upgrade mit Vorab-Backup, und ein
  Upgrade mit absichtlich falschem Zugang, das gesperrt werden muss.
