package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// One archive instead of two (etappe 72).
//
// E68 and E69 produced a configuration archive; E70 added a separate homeserver dump.
// Both were right on their own and wrong together: the Backup page showed the order the
// features were built in rather than the operator's task, with three warning blocks
// explaining what each half could not do.
//
// "the backup" should be one thing you can hold.

// FullManifest covers every part in a combined archive.
type FullManifest struct {
	FormatVersion int       `json:"format_version"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`
	ESS           Release   `json:"ess"`
	// Parts is what is inside, in the order it was written.
	Parts []string `json:"parts"`
	// Config and Homeserver are the two sub-manifests, kept whole so each part can still
	// be read on its own terms.
	Config     Manifest            `json:"config"`
	Homeserver *HomeserverManifest `json:"homeserver,omitempty"`
	// NotIncluded is now one line rather than three warnings on a screen.
	NotIncluded []string `json:"not_included"`
}

// CreateFull writes configuration, MatrixCtrl's database and Synapse's database into a
// single archive.
//
// The homeserver connection is optional: without cluster access there is no way to reach
// Synapse's database, and half an archive with a manifest that says so beats refusing to
// produce one at all.
// FullOptions is what goes into a complete archive.
//
// A struct rather than eight positional parameters: the archive grew from two parts to
// five (etappe 102), and a call site that passes three nils in a row is a call site
// nobody can read.
type FullOptions struct {
	// DB is MatrixCtrl's own database. Required.
	DB *pgxpool.Pool
	// Homeserver is Synapse's database — rooms, messages, devices. Nil leaves it out
	// and the manifest says so.
	Homeserver *pgx.Conn
	// MAS is the authentication service's database, which is where the **accounts**
	// live under MSC3861. Leaving it out of a "full" archive was the gap that made a
	// migration impossible: rooms came back and nobody could log in to them.
	MAS *pgx.Conn
	// Media streams the uploaded files. Nil leaves them out — they can be hundreds of
	// gigabytes, so this is the operator's choice, made against a measured number.
	Media io.Reader
	// Sealed is the encrypted secrets part: signing key, macaroon, MAS encryption
	// secret. Nil leaves it out. Encrypted because whoever holds it holds the
	// homeserver; included by default because without it a restore keeps no sessions.
	Sealed     []byte
	ConfigRepo string
	AppVersion string
	ESS        Release
}

func CreateFull(ctx context.Context, opts FullOptions, w io.Writer) error {
	db, hs, configRepo, appVersion, ess := opts.DB, opts.Homeserver, opts.ConfigRepo, opts.AppVersion, opts.ESS

	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	tables, err := tablesWithRows(ctx, db)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	configFiles, ferr := countFiles(configRepo)
	if ferr != nil {
		configFiles = -1
	}

	cfgMan := Manifest{
		FormatVersion: FormatVersion,
		CreatedAt:     time.Now().UTC(),
		AppVersion:    appVersion,
		ESS:           ess,
		Tables:        tables,
		ConfigFiles:   configFiles,
		Restores:      configRestores,
		SchemaNote:    schemaNote,
	}

	full := FullManifest{
		FormatVersion: FormatVersion,
		CreatedAt:     cfgMan.CreatedAt,
		AppVersion:    appVersion,
		ESS:           ess,
		Parts:         []string{"matrixctrl/"},
		Config:        cfgMan,
		NotIncluded:   []string{},
	}
	if hs != nil {
		full.Parts = append(full.Parts, "homeserver/")
	} else {
		full.NotIncluded = append(full.NotIncluded,
			"Synapses Datenbank — dieser Lauf hatte keinen Zugriff darauf.")
	}
	if opts.MAS != nil {
		full.Parts = append(full.Parts, "mas/")
	} else {
		full.NotIncluded = append(full.NotIncluded,
			"Die Konten — sie liegen in der Datenbank des Matrix Authentication Service, "+
				"und dieser Lauf hatte keinen Zugriff darauf.")
	}
	if opts.Media != nil {
		full.Parts = append(full.Parts, "media/")
	} else {
		full.NotIncluded = append(full.NotIncluded,
			"Die hochgeladenen Dateien — beim Erstellen nicht ausgewählt.")
	}
	if len(opts.Sealed) > 0 {
		full.Parts = append(full.Parts, "secrets/")
	} else {
		full.NotIncluded = append(full.NotIncluded,
			"Die Schlüssel des Homeservers — ohne sie ist nach dem Zurückspielen jede "+
				"bestehende Sitzung ungültig und die Konten-Datenbank nicht entschlüsselbar.")
	}

	// The combined manifest first, so anything reading the stream knows what is coming.
	blob, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(tw, "manifest.json", blob, 0o644, full.CreatedAt); err != nil {
		return err
	}

	// Part one: what MatrixCtrl owns. Prefixed, so each part stays readable on its own
	// and a restore can tell them apart without guessing from table names.
	dump := func(t Table) ([]byte, error) { return copyTable(ctx, db, t) }
	if err := assembleUnder(tw, "matrixctrl/", cfgMan, dump, configRepo, configFiles >= 0); err != nil {
		return err
	}

	// Part two: the homeserver itself.
	if hs != nil {
		if err := exportDatabaseUnder(ctx, tw, "homeserver/", hs, "synapse", full.CreatedAt,
			[]string{"Die hochgeladenen Dateien (Media-Volume) — sie sind ein eigener Teil dieses Archivs."}); err != nil {
			return fmt.Errorf("homeserver: %w", err)
		}
	}

	// Part three: the accounts. Same exporter, different database — it was only ever
	// called with Synapse's, which is why a "full" archive had no users in it.
	if opts.MAS != nil {
		if err := exportDatabaseUnder(ctx, tw, "mas/", opts.MAS, "matrixauthenticationservice", full.CreatedAt,
			[]string{"Nichts — diese Datenbank ist hier vollständig."}); err != nil {
			return fmt.Errorf("mas: %w", err)
		}
	}

	// Part four: the uploaded files, streamed straight through. They are already a tar
	// coming out of the Synapse pod, so they are stored as one member rather than
	// unpacked and repacked — on a large install that is the difference between a
	// stream and a disk.
	if opts.Media != nil {
		if err := writeStream(tw, "media/media.tar", opts.Media, full.CreatedAt); err != nil {
			return fmt.Errorf("media: %w", err)
		}
	}

	// Part five: the sealed secrets.
	if len(opts.Sealed) > 0 {
		if err := writeFile(tw, "secrets/sealed.bin", opts.Sealed, 0o600, full.CreatedAt); err != nil {
			return fmt.Errorf("secrets: %w", err)
		}
	}
	return nil
}

// writeStream copies a reader into the archive without holding it in memory.
//
// tar needs the size in the header before the body, and a stream does not know it — so
// it is buffered to a temporary file first, which is still not the whole archive in RAM
// and is the only honest way to tar something of unknown length.
func writeStream(tw *tar.Writer, name string, r io.Reader, at time.Time) error {
	tmp, err := os.CreateTemp("", "mxctrl-part-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	n, err := io.Copy(tmp, r)
	if err != nil {
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o644, Size: n, ModTime: at, Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	_, err = io.Copy(tw, tmp)
	return err
}

// configRestores and schemaNote are shared with Create so the two archives describe the
// same contents in the same words (rule 3).
var configRestores = []string{
	"Die vollständige ESS-Konfiguration mit Git-Historie: Hostnames, serverName, TLS-Issuer, RTC-Einstellungen.",
	"Die Hooks, die manuelle Patches nach jedem Upgrade und Rollback wiederherstellen.",
	"Upgrade-Verlauf, Melde-Entscheidungen und den aufgezeichneten Node-Verlauf.",
}

const schemaNote = "Enthält nur Daten, kein Schema: das Schema entsteht beim Zurückspielen aus den Migrationen, " +
	"damit ein Archiv auf den aktuellen Stand zurückkommt und nicht auf den, bei dem es entstanden ist."

// assembleUnder is assemble with every path prefixed.
func assembleUnder(tw *tar.Writer, prefix string, man Manifest,
	dump func(Table) ([]byte, error), configRepo string, withConfig bool) error {

	blob, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(tw, prefix+"manifest.json", blob, 0o644, man.CreatedAt); err != nil {
		return err
	}
	for _, t := range man.Tables {
		data, err := dump(t)
		if err != nil {
			return fmt.Errorf("dump %s: %w", t.Name, err)
		}
		if err := writeFile(tw, prefix+t.File, data, 0o644, man.CreatedAt); err != nil {
			return err
		}
	}
	if withConfig {
		return addTree(tw, configRepo, prefix+"config-repo")
	}
	return nil
}

const homeserverRestoreNote = "Zurückspielen ist bewusst kein Knopf: dafür muss Synapse gestoppt sein, " +
	"und es im laufenden Betrieb zu tun beschädigt, was da ist. Das Archiv ist eine " +
	"Datei, die bewusst mit psql eingespielt wird."
