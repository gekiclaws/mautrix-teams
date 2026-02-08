package teamsbridge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type ThreadRegistration struct {
	Thread model.Thread
	RoomID id.RoomID
}

// RefreshAndRegisterThreads is idempotent. It only registers threads that do
// not already exist in the store.
func RefreshAndRegisterThreads(ctx context.Context, discoverer *TeamsThreadDiscoverer, store ThreadStore, rooms *RoomsService, log zerolog.Logger) (int, []ThreadRegistration, error) {
	if discoverer == nil {
		return 0, nil, errors.New("missing discoverer")
	}
	if store == nil {
		return 0, nil, errors.New("missing thread store")
	}
	if rooms == nil {
		return 0, nil, errors.New("missing rooms service")
	}

	threads, err := discoverer.Discover(ctx)
	if err != nil {
		return 0, nil, err
	}
	discovered := len(threads)

	registrations := make([]ThreadRegistration, 0, len(threads))
	for _, thread := range threads {
		roomID, created, err := rooms.EnsureRoom(thread)
		if err != nil {
			log.Error().
				Err(err).
				Str("thread_id", thread.ID).
				Msg("failed to ensure room for discovered thread")
			return discovered, nil, err
		}
		if !created {
			continue
		}
		registrations = append(registrations, ThreadRegistration{
			Thread: thread,
			RoomID: roomID,
		})
	}

	return discovered, registrations, nil
}
