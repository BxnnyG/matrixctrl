package auth

import (
	"context"
	"log"
	"time"

	"github.com/bxnnyg/matrixctrl/internal/mas"
)

// SweepMASSessions ends, once at start and then hourly, the MAS sessions of MatrixCtrl's
// own client that nothing uses any more (etappe 119d): sign-ins older than a few
// minutes, Matrix grants idle for a day — every restart orphaned one — and MatrixCtrl's
// own spent admin tokens. Revoking at the source stops new ones piling up; this ends
// what is already there and what a restart leaves behind, which no in-memory record
// can revoke.
func SweepMASSessions(ctx context.Context, current func() *OIDCService) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	first := time.NewTimer(2 * time.Minute) // let OIDC come up after a start
	defer first.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-t.C:
		}
		o := current()
		if o == nil || o.MAS() == nil {
			continue
		}
		sweepOnce(ctx, o.MAS(), o.ClientID())
	}
}

func sweepOnce(ctx context.Context, c *mas.Client, clientID string) {
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	sessions, err := c.ActiveOAuth2Sessions(sctx, clientID)
	if err != nil {
		log.Printf("mas sessions: cannot list: %v", err)
		return
	}
	stale := mas.StaleSessions(sessions, clientID, time.Now())
	ended := 0
	for _, s := range stale {
		if err := c.FinishOAuth2Session(sctx, s.ID); err != nil {
			log.Printf("mas sessions: cannot end %s: %v", s.ID, err)
			continue
		}
		ended++
	}
	if ended > 0 {
		log.Printf("mas sessions: ended %d of %d active MatrixCtrl session(s) nothing uses any more", ended, len(sessions))
	}
}
