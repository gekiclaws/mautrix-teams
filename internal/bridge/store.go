package teamsbridge

import (
	"sync"

	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
)

type ThreadStore interface {
	Get(threadID string) (id.RoomID, bool)
	Put(threadID string, roomID id.RoomID) error
}

type TeamsThreadStore struct {
	db         *database.Database
	mu         sync.RWMutex
	byThreadID map[string]id.RoomID
}

func NewTeamsThreadStore(db *database.Database) *TeamsThreadStore {
	return &TeamsThreadStore{
		db:         db,
		byThreadID: make(map[string]id.RoomID),
	}
}

func (s *TeamsThreadStore) LoadAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byThreadID = make(map[string]id.RoomID)
	if s.db == nil || s.db.TeamsThread == nil {
		return
	}
	for _, row := range s.db.TeamsThread.GetAll() {
		if row == nil {
			continue
		}
		s.byThreadID[row.ThreadID] = row.RoomID
	}
}

func (s *TeamsThreadStore) Get(threadID string) (id.RoomID, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	roomID, ok := s.byThreadID[threadID]
	return roomID, ok
}

func (s *TeamsThreadStore) Put(threadID string, roomID id.RoomID) error {
	if s.db == nil || s.db.TeamsThread == nil {
		return nil
	}
	record := s.db.TeamsThread.New()
	record.ThreadID = threadID
	record.RoomID = roomID
	if err := record.Upsert(); err != nil {
		return err
	}
	s.mu.Lock()
	s.byThreadID[threadID] = roomID
	s.mu.Unlock()
	return nil
}
