package notify

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const settingsKey = "notifications"

type pgSettings struct{ db *pgxpool.Pool }

// NewPGSettings keeps Settings as one JSON row in instance_settings. Without a row,
// everything is off.
func NewPGSettings(db *pgxpool.Pool) SettingsStore { return &pgSettings{db: db} }

func (p *pgSettings) Load(ctx context.Context) (Settings, error) {
	var raw string
	err := p.db.QueryRow(ctx, `SELECT value FROM instance_settings WHERE key=$1`, settingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Default(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	s := Default()
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Settings{}, err
	}
	return s.Normalize(), nil
}

func (p *pgSettings) Save(ctx context.Context, s Settings) error {
	blob, err := json.Marshal(s.Normalize())
	if err != nil {
		return err
	}
	_, err = p.db.Exec(ctx, `
		INSERT INTO instance_settings(key, value) VALUES($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		settingsKey, string(blob))
	return err
}

// SettingsSaved reports whether a row exists (the page explains "everything off" when not).
func SettingsSaved(ctx context.Context, db *pgxpool.Pool) bool {
	var n int
	_ = db.QueryRow(ctx, `SELECT count(*) FROM instance_settings WHERE key=$1`, settingsKey).Scan(&n)
	return n > 0
}

type pgStore struct{ db *pgxpool.Pool }

// NewPGStore keeps the state in notify_state and notify_channels.
func NewPGStore(db *pgxpool.Pool) Store { return &pgStore{db: db} }

func (p *pgStore) Rows(ctx context.Context) ([]Row, error) {
	rows, err := p.db.Query(ctx, `SELECT key, kind, title, detail, once, since, last_sent, resolved_at FROM notify_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Key, &r.Trigger, &r.Title, &r.Detail, &r.Once, &r.Since, &r.LastSent, &r.Resolved); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *pgStore) SaveRow(ctx context.Context, r Row) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO notify_state(key, kind, title, detail, once, since, last_sent, resolved_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (key) DO UPDATE SET kind=EXCLUDED.kind, title=EXCLUDED.title, detail=EXCLUDED.detail,
			once=EXCLUDED.once, last_sent=EXCLUDED.last_sent, resolved_at=EXCLUDED.resolved_at`,
		r.Key, r.Trigger, r.Title, r.Detail, r.Once, r.Since, r.LastSent, r.Resolved)
	return err
}

func (p *pgStore) DeleteRow(ctx context.Context, key string) error {
	_, err := p.db.Exec(ctx, `DELETE FROM notify_state WHERE key=$1`, key)
	return err
}

func (p *pgStore) Channels(ctx context.Context) ([]ChannelStatus, error) {
	rows, err := p.db.Query(ctx, `SELECT channel, last_sent_at, last_error, last_error_at FROM notify_channels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelStatus
	for rows.Next() {
		var c ChannelStatus
		if err := rows.Scan(&c.Channel, &c.LastSentAt, &c.LastError, &c.LastErrorAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (p *pgStore) SaveChannel(ctx context.Context, c ChannelStatus) error {
	_, err := p.db.Exec(ctx, `
		INSERT INTO notify_channels(channel, last_sent_at, last_error, last_error_at) VALUES($1,$2,$3,$4)
		ON CONFLICT (channel) DO UPDATE SET last_sent_at=EXCLUDED.last_sent_at,
			last_error=EXCLUDED.last_error, last_error_at=EXCLUDED.last_error_at`,
		c.Channel, c.LastSentAt, c.LastError, c.LastErrorAt)
	return err
}
