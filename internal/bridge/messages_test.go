package teamsbridge

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/database"
	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type fakeMessageLister struct {
	messages       []model.RemoteMessage
	err            error
	conversationID string
	since          string
}

func (f *fakeMessageLister) ListMessages(ctx context.Context, conversationID string, sinceSequence string) ([]model.RemoteMessage, error) {
	f.conversationID = conversationID
	f.since = sinceSequence
	return f.messages, f.err
}

type fakeMatrixSender struct {
	failBody string
	sent     []string
	extra    []map[string]any
}

func (f *fakeMatrixSender) SendText(roomID id.RoomID, body string, extra map[string]any) (id.EventID, error) {
	if body == f.failBody {
		return "", errors.New("send failed")
	}
	f.sent = append(f.sent, body)
	f.extra = append(f.extra, extra)
	return id.EventID("$event"), nil
}

type fakeProfileStore struct {
	byID        map[string]*database.TeamsProfile
	insertedIDs []string
}

func (f *fakeProfileStore) GetByTeamsUserID(teamsUserID string) *database.TeamsProfile {
	if f == nil || f.byID == nil {
		return nil
	}
	return f.byID[teamsUserID]
}

func (f *fakeProfileStore) InsertIfMissing(profile *database.TeamsProfile) (bool, error) {
	if f.byID == nil {
		f.byID = make(map[string]*database.TeamsProfile)
	}
	if _, exists := f.byID[profile.TeamsUserID]; exists {
		return false, nil
	}
	f.byID[profile.TeamsUserID] = profile
	f.insertedIDs = append(f.insertedIDs, profile.TeamsUserID)
	return true, nil
}

func TestIngestThreadFiltersBySequence(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", Body: "one"},
			{SequenceID: "2", Body: "two"},
			{SequenceID: "3", Body: "three"},
		},
	}
	sender := &fakeMatrixSender{}
	ingestor := &MessageIngestor{
		Lister: lister,
		Sender: sender,
		Log:    zerolog.New(io.Discard),
	}

	last := "2"
	seq, advanced, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", &last)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !advanced {
		t.Fatalf("expected advancement when newer message is sent")
	}
	if seq != "3" {
		t.Fatalf("unexpected seq: %q", seq)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "three" {
		t.Fatalf("unexpected sent messages: %#v", sender.sent)
	}
}

func TestIngestThreadStopsOnFailure(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", Body: "one"},
			{SequenceID: "2", Body: "two"},
			{SequenceID: "3", Body: "three"},
		},
	}
	sender := &fakeMatrixSender{failBody: "two"}
	ingestor := &MessageIngestor{
		Lister: lister,
		Sender: sender,
		Log:    zerolog.New(io.Discard),
	}

	seq, advanced, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if advanced {
		t.Fatalf("expected no advancement on failure")
	}
	if seq != "" {
		t.Fatalf("unexpected seq: %q", seq)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "one" {
		t.Fatalf("expected only first message sent, got: %#v", sender.sent)
	}
}

func TestIngestThreadAdvancesOnSuccess(t *testing.T) {
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

	seq, advanced, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !advanced {
		t.Fatalf("expected advancement on success")
	}
	if seq != "2" {
		t.Fatalf("unexpected seq: %q", seq)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("expected both messages sent, got: %#v", sender.sent)
	}
}

func TestIngestThreadInsertsProfileAndUsesDisplayName(t *testing.T) {
	now := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "user-1", SenderName: "User One", Timestamp: now, Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	store := &fakeProfileStore{}
	ingestor := &MessageIngestor{
		Lister:   lister,
		Sender:   sender,
		Profiles: store,
		Log:      zerolog.New(io.Discard),
	}

	_, advanced, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(store.insertedIDs) != 1 || store.insertedIDs[0] != "user-1" {
		t.Fatalf("expected profile insert for user-1, got %#v", store.insertedIDs)
	}
	profile := store.byID["user-1"]
	if profile == nil || profile.DisplayName != "User One" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	if len(sender.extra) != 1 {
		t.Fatalf("expected one message send, got %d", len(sender.extra))
	}
	perMessage, ok := sender.extra[0]["com.beeper.per_message_profile"].(map[string]any)
	if !ok {
		t.Fatalf("missing per-message profile metadata")
	}
	if perMessage["id"] != "user-1" || perMessage["displayname"] != "User One" {
		t.Fatalf("unexpected per-message profile: %#v", perMessage)
	}
}

func TestIngestThreadUsesCachedDisplayName(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "user-2", SenderName: "Payload Name", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	store := &fakeProfileStore{
		byID: map[string]*database.TeamsProfile{
			"user-2": {
				TeamsUserID: "user-2",
				DisplayName: "Cached Name",
				LastSeenTS:  time.Now().UTC(),
			},
		},
	}
	ingestor := &MessageIngestor{
		Lister:   lister,
		Sender:   sender,
		Profiles: store,
		Log:      zerolog.New(io.Discard),
	}

	_, advanced, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(store.insertedIDs) != 0 {
		t.Fatalf("expected no profile insert, got %#v", store.insertedIDs)
	}
	perMessage, ok := sender.extra[0]["com.beeper.per_message_profile"].(map[string]any)
	if !ok {
		t.Fatalf("missing per-message profile metadata")
	}
	if perMessage["displayname"] != "Cached Name" {
		t.Fatalf("unexpected per-message display name: %#v", perMessage["displayname"])
	}
}
