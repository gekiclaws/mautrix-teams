package teamsbridge

import (
	"context"
	"io"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type fakeProgressStore struct {
	seqByThread map[string]string
	calls       int
	err         error
}

func (f *fakeProgressStore) UpdateLastSequenceID(threadID string, seq string) error {
	f.calls++
	if f.seqByThread == nil {
		f.seqByThread = make(map[string]string)
	}
	f.seqByThread[threadID] = seq
	return f.err
}

func TestSyncThreadNoResendOnSecondRun(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", Body: "one"},
			{SequenceID: "2", Body: "two"},
		},
	}
	sender := &fakeMatrixSender{}
	ingestor := &MessageIngestor{
		Lister: lister,
		Sender: sender,
		Log:    zerolog.New(io.Discard),
	}
	store := &fakeProgressStore{}
	syncer := &ThreadSyncer{
		Ingestor: ingestor,
		Store:    store,
		Log:      zerolog.New(io.Discard),
	}
	thread := &database.TeamsThread{
		ThreadID:       "thread-1",
		RoomID:         id.RoomID("!room:example"),
		ConversationID: stringPtr("@thread.v2"),
	}

	if err := syncer.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("SyncThread failed: %v", err)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(sender.sent))
	}
	if store.calls != 1 || store.seqByThread["thread-1"] != "2" {
		t.Fatalf("expected persisted seq 2, got %#v", store.seqByThread)
	}

	last := store.seqByThread["thread-1"]
	thread.LastSequenceID = &last
	if err := syncer.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("SyncThread second run failed: %v", err)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("expected no additional sends, got %d", len(sender.sent))
	}
	if store.calls != 1 {
		t.Fatalf("expected no additional persistence calls, got %d", store.calls)
	}
}

func TestSyncThreadStopsOnFailure(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", Body: "one"},
			{SequenceID: "2", Body: "two"},
		},
	}
	sender := &fakeMatrixSender{failBody: "two"}
	ingestor := &MessageIngestor{
		Lister: lister,
		Sender: sender,
		Log:    zerolog.New(io.Discard),
	}
	store := &fakeProgressStore{}
	syncer := &ThreadSyncer{
		Ingestor: ingestor,
		Store:    store,
		Log:      zerolog.New(io.Discard),
	}
	thread := &database.TeamsThread{
		ThreadID:       "thread-1",
		RoomID:         id.RoomID("!room:example"),
		ConversationID: stringPtr("@thread.v2"),
	}

	if err := syncer.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("SyncThread failed: %v", err)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "one" {
		t.Fatalf("expected only first message sent, got: %#v", sender.sent)
	}
	if store.calls != 0 {
		t.Fatalf("expected no persistence on failure, got %d", store.calls)
	}
}

func TestSyncThreadResumesAfterFailure(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", Body: "one"},
			{SequenceID: "2", Body: "two"},
			{SequenceID: "3", Body: "three"},
		},
	}
	failSender := &fakeMatrixSender{failBody: "two"}
	ingestor := &MessageIngestor{
		Lister: lister,
		Sender: failSender,
		Log:    zerolog.New(io.Discard),
	}
	store := &fakeProgressStore{}
	syncer := &ThreadSyncer{
		Ingestor: ingestor,
		Store:    store,
		Log:      zerolog.New(io.Discard),
	}
	thread := &database.TeamsThread{
		ThreadID:       "thread-1",
		RoomID:         id.RoomID("!room:example"),
		ConversationID: stringPtr("@thread.v2"),
	}

	if err := syncer.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("SyncThread failed: %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("expected no persistence on failure, got %d", store.calls)
	}

	retrySender := &fakeMatrixSender{}
	syncer.Ingestor.Sender = retrySender
	if err := syncer.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("SyncThread retry failed: %v", err)
	}
	if len(retrySender.sent) != 3 {
		t.Fatalf("expected retry to send all messages, got %d", len(retrySender.sent))
	}
	if store.calls != 1 || store.seqByThread["thread-1"] != "3" {
		t.Fatalf("expected persistence after retry, got %#v", store.seqByThread)
	}
}

func TestSyncThreadSkipsEmptyBody(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", Body: "one"},
			{SequenceID: "2", Body: ""},
			{SequenceID: "3", Body: "three"},
		},
	}
	sender := &fakeMatrixSender{}
	ingestor := &MessageIngestor{
		Lister: lister,
		Sender: sender,
		Log:    zerolog.New(io.Discard),
	}
	store := &fakeProgressStore{}
	syncer := &ThreadSyncer{
		Ingestor: ingestor,
		Store:    store,
		Log:      zerolog.New(io.Discard),
	}
	thread := &database.TeamsThread{
		ThreadID:       "thread-1",
		RoomID:         id.RoomID("!room:example"),
		ConversationID: stringPtr("@thread.v2"),
	}

	if err := syncer.SyncThread(context.Background(), thread); err != nil {
		t.Fatalf("SyncThread failed: %v", err)
	}
	if len(sender.sent) != 2 || sender.sent[0] != "one" || sender.sent[1] != "three" {
		t.Fatalf("unexpected sends: %#v", sender.sent)
	}
	if store.calls != 1 || store.seqByThread["thread-1"] != "3" {
		t.Fatalf("expected persistence of last non-empty sequence, got %#v", store.seqByThread)
	}
}

func stringPtr(value string) *string {
	return &value
}
