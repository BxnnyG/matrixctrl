package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ResyncSequences moves every sequence that feeds a column of this schema past the
// highest value that column holds (etappe 109).
//
// Rows copied into a database — a move to a new server, a restore, a hand-run
// pg_dump --data-only — arrive without their sequences. The next INSERT then draws an
// id that already exists, and every one after it too: on the migrated installation
// the node and RTC history failed once a minute with `duplicate key … _pkey` for a day
// before anyone looked. Nothing was lost, nothing was recorded.
//
// Only ever forward, and only when behind: a sequence ahead of its column is left
// alone (gaps are harmless, reuse is not). Idempotent, so it runs on every start.
// Returns the sequences it moved.
func ResyncSequences(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT seq.relname, tbl.relname, col.attname
		FROM pg_class seq
		JOIN pg_depend dep  ON dep.objid = seq.oid AND dep.deptype IN ('a', 'i')
		JOIN pg_class tbl   ON tbl.oid = dep.refobjid
		JOIN pg_attribute col ON col.attrelid = tbl.oid AND col.attnum = dep.refobjsubid
		JOIN pg_namespace ns ON ns.oid = seq.relnamespace
		WHERE seq.relkind = 'S' AND ns.nspname = current_schema()
		ORDER BY seq.relname`)
	if err != nil {
		return nil, err
	}
	type owned struct{ seq, table, column string }
	var all []owned
	for rows.Next() {
		var o owned
		if err := rows.Scan(&o.seq, &o.table, &o.column); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var moved []string
	for _, o := range all {
		seq := pgx.Identifier{o.seq}.Sanitize()
		q := fmt.Sprintf(`
			SELECT setval('%s', m)
			FROM (SELECT MAX(%s) AS m FROM %s) cur,
			     (SELECT CASE WHEN is_called THEN last_value + 1 ELSE last_value END AS next FROM %s) s
			WHERE cur.m IS NOT NULL AND cur.m >= s.next`,
			// setval takes the sequence as a regclass literal.
			pgx.Identifier{o.seq}.Sanitize(), pgx.Identifier{o.column}.Sanitize(),
			pgx.Identifier{o.table}.Sanitize(), seq)
		tag, err := pool.Exec(ctx, q)
		if err != nil {
			return moved, fmt.Errorf("%s: %w", o.seq, err)
		}
		if tag.RowsAffected() > 0 {
			moved = append(moved, o.seq)
		}
	}
	return moved, nil
}
