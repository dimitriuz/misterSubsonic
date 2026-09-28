package player

import (
	"context"
	"time"

	"mistersubsonic/internal/subsonic"
)

// API is the subset of *subsonic.Client the player uses.
type API interface {
	Scrobble(ctx context.Context, id subsonic.ID, at time.Time, submission bool) error
	SavePlayQueue(ctx context.Context, ids []subsonic.ID, current subsonic.ID, pos time.Duration) error
	GetPlayQueue(ctx context.Context) (*subsonic.PlayQueue, error)
}
