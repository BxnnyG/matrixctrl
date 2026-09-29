package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Rows copied in without their sequence — what a move to a new server did to the node
// history. The first insert after it collided with an existing id; after the resync it
// must not. The counter-probe: a sequence already ahead is not moved back, and a second
// run moves nothing.
func TestResyncSequencesAfterCopiedRows(t *testing.T) {
	dsn := os.Getenv("MATRIXCTRL_TEST_DB")
	if dsn == "" {
		t.Skip("set MATRIXCTRL_TEST_DB to a throwaway database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, q := range []string{
		`DROP TABLE IF EXISTS seqprobe_behind, seqprobe_ahead`,
		`CREATE TABLE seqprobe_behind (id BIGSERIAL PRIMARY KEY, v TEXT)`,
		`CREATE TABLE seqprobe_ahead  (id BIGSERIAL PRIMARY KEY, v TEXT)`,
		// Copied rows: explicit ids, sequence untouched.
		`INSERT INTO seqprobe_behind (id, v) VALUES (1,'a'), (2,'b'), (3,'c')`,
		`INSERT INTO seqprobe_ahead (v) SELECT 'x' FROM generate_series(1,5)`,
		`DELETE FROM seqprobe_ahead WHERE id > 2`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	defer pool.Exec(ctx, `DROP TABLE IF EXISTS seqprobe_behind, seqprobe_ahead`)

	// Reproduce first: the insert collides.
	if _, err := pool.Exec(ctx, `INSERT INTO seqprobe_behind (v) VALUES ('new')`); err == nil {
		t.Fatal("the copied rows should make the next insert collide — the setup does not reproduce the bug")
	}

	moved, err := ResyncSequences(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(moved, "seqprobe_behind_id_seq") || contains(moved, "seqprobe_ahead_id_seq") {
		t.Errorf("moved %v, want the behind sequence and not the ahead one", moved)
	}
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO seqprobe_behind (v) VALUES ('new') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("insert after resync: %v", err)
	}
	if id != 4 {
		t.Errorf("next id %d, want 4", id)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO seqprobe_ahead (v) VALUES ('new') RETURNING id`).Scan(&id); err != nil || id != 6 {
		t.Errorf("a sequence ahead of its rows must not move back: id=%d err=%v", id, err)
	}
	if again, _ := ResyncSequences(ctx, pool); contains(again, "seqprobe_behind_id_seq") || contains(again, "seqprobe_ahead_id_seq") {
		t.Errorf("a second run should move nothing of ours: %v", again)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
