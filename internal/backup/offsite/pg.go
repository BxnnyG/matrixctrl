package offsite

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const settingsKey = "backup.offsite"

type pgSettings struct{ db *pgxpool.Pool }

// NewPGSettings keeps Settings as one JSON row in instance_settings.
func NewPGSettings(db *pgxpool.Pool) SettingsStore { return &pgSettings{db: db} }

func (p *pgSettings) Load(ctx context.Context) (Settings, error) {
	var raw string
	err := p.db.QueryRow(ctx, `SELECT value FROM instance_settings WHERE key=$1`, settingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	s := DefaultSettings()
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func (p *pgSettings) Save(ctx context.Context, s Settings) error {
	blob, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(ctx, `
		INSERT INTO instance_settings(key, value) VALUES($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		settingsKey, string(blob))
	return err
}

type pgRuns struct{ db *pgxpool.Pool }

// NewPGRuns keeps the run history in offsite_runs.
func NewPGRuns(db *pgxpool.Pool) RunStore { return &pgRuns{db: db} }

const runColumns = `id, started_at, finished_at, occasion, status, object_key, size_bytes, media, error, note`

func scanRun(row pgx.Row) (RunRecord, error) {
	var r RunRecord
	err := row.Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Occasion, &r.Status, &r.Object, &r.Size, &r.Media, &r.Error, &r.Note)
	return r, err
}

func (p *pgRuns) Insert(ctx context.Context, r RunRecord) (int64, error) {
	var id int64
	err := p.db.QueryRow(ctx, `
		INSERT INTO offsite_runs(started_at, finished_at, occasion, status, object_key, size_bytes, media, error, note)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		r.StartedAt, r.FinishedAt, r.Occasion, r.Status, r.Object, r.Size, r.Media, r.Error, r.Note).Scan(&id)
	return id, err
}

func (p *pgRuns) Update(ctx context.Context, r RunRecord) error {
	_, err := p.db.Exec(ctx, `
		UPDATE offsite_runs SET finished_at=$2, status=$3, object_key=$4, size_bytes=$5, error=$6, note=$7
		WHERE id=$1`,
		r.ID, r.FinishedAt, r.Status, r.Object, r.Size, r.Error, r.Note)
	return err
}

func (p *pgRuns) Recent(ctx context.Context, n int) ([]RunRecord, error) {
	rows, err := p.db.Query(ctx, `SELECT `+runColumns+` FROM offsite_runs ORDER BY started_at DESC, id DESC LIMIT $1`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *pgRuns) LastOK(ctx context.Context) (*RunRecord, error) {
	r, err := scanRun(p.db.QueryRow(ctx,
		`SELECT `+runColumns+` FROM offsite_runs WHERE status='ok' ORDER BY started_at DESC, id DESC LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (p *pgRuns) LastStart(ctx context.Context, occasion string) (time.Time, error) {
	var t *time.Time
	err := p.db.QueryRow(ctx, `SELECT max(started_at) FROM offsite_runs WHERE occasion=$1`, occasion).Scan(&t)
	if err != nil || t == nil {
		return time.Time{}, err
	}
	return *t, nil
}

func (p *pgRuns) CloseInterrupted(ctx context.Context) (int64, error) {
	tag, err := p.db.Exec(ctx, `
		UPDATE offsite_runs SET status='failed', finished_at=now(),
		       error='Durch einen Neustart von MatrixCtrl unterbrochen.'
		WHERE status='running'`)
	return tag.RowsAffected(), err
}
