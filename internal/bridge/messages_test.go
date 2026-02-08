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
	updatedIDs  []string
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

func (f *fakeProfileStore) UpdateDisplayName(teamsUserID string, displayName string, lastSeenTS time.Time) error {
	if f.byID == nil {
		f.byID = make(map[string]*database.TeamsProfile)
	}
	profile := f.byID[teamsUserID]
	if profile == nil {
		profile = &database.TeamsProfile{TeamsUserID: teamsUserID}
		f.byID[teamsUserID] = profile
	}
	profile.DisplayName = displayName
	profile.LastSeenTS = lastSeenTS
	f.updatedIDs = append(f.updatedIDs, teamsUserID)
	return nil
}

type fakeSendIntentLookup struct {
	byClientMessageID map[string]*database.TeamsSendIntent
}

func (f *fakeSendIntentLookup) GetByClientMessageID(clientMessageID string) *database.TeamsSendIntent {
	if f == nil || f.byClientMessageID == nil {
		return nil
	}
	return f.byClientMessageID[clientMessageID]
}

type fakeMessageMapWriter struct {
	entries []*database.TeamsMessageMap
}

func (f *fakeMessageMapWriter) Upsert(mapping *database.TeamsMessageMap) error {
	f.entries = append(f.entries, mapping)
	return nil
}

type fakeReactionIngestor struct {
	messageIDs  []string
	targetMXIDs []id.EventID
}

func (f *fakeReactionIngestor) IngestMessageReactions(ctx context.Context, threadID string, roomID id.RoomID, msg model.RemoteMessage, targetMXID id.EventID) error {
	f.messageIDs = append(f.messageIDs, msg.MessageID)
	f.targetMXIDs = append(f.targetMXIDs, targetMXID)
	return nil
}

type fakeUnreadTracker struct {
	rooms []id.RoomID
}

func (f *fakeUnreadTracker) MarkUnread(roomID id.RoomID) {
	f.rooms = append(f.rooms, roomID)
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
	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", &last)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement when newer message is sent")
	}
	if res.LastSequenceID != "3" {
		t.Fatalf("unexpected seq: %q", res.LastSequenceID)
	}
	if res.MessagesIngested != 1 {
		t.Fatalf("expected 1 ingested message, got %d", res.MessagesIngested)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "three" {
		t.Fatalf("unexpected sent messages: %#v", sender.sent)
	}
}

func TestIngestThreadAlwaysIngestsReactions(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{MessageID: "m1", SequenceID: "1", Body: "one"},
			{MessageID: "m2", SequenceID: "3", Body: ""},
		},
	}
	sender := &fakeMatrixSender{}
	reactions := &fakeReactionIngestor{}
	ingestor := &MessageIngestor{
		Lister:           lister,
		Sender:           sender,
		ReactionIngestor: reactions,
		Log:              zerolog.New(io.Discard),
	}

	last := "2"
	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", &last)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if res.Advanced {
		t.Fatalf("unexpected advance")
	}
	if res.MessagesIngested != 0 {
		t.Fatalf("expected 0 ingested messages, got %d", res.MessagesIngested)
	}
	if len(reactions.messageIDs) != 2 {
		t.Fatalf("expected 2 reaction ingests, got %d", len(reactions.messageIDs))
	}
	if reactions.messageIDs[0] != "m1" || reactions.messageIDs[1] != "m2" {
		t.Fatalf("unexpected reaction ingest order: %#v", reactions.messageIDs)
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if res.Advanced {
		t.Fatalf("expected no advancement on failure")
	}
	if res.LastSequenceID != "" {
		t.Fatalf("unexpected seq: %q", res.LastSequenceID)
	}
	if res.MessagesIngested != 0 {
		t.Fatalf("expected 0 ingested messages on failure, got %d", res.MessagesIngested)
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if res.LastSequenceID != "2" {
		t.Fatalf("unexpected seq: %q", res.LastSequenceID)
	}
	if res.MessagesIngested != 2 {
		t.Fatalf("expected 2 ingested messages, got %d", res.MessagesIngested)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("expected both messages sent, got: %#v", sender.sent)
	}
}

func TestIngestThreadInsertsProfileAndUsesDisplayName(t *testing.T) {
	now := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "8:user-1", IMDisplayName: "User One", Timestamp: now, Body: "one"},
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(store.insertedIDs) != 1 || store.insertedIDs[0] != "8:user-1" {
		t.Fatalf("expected profile insert for user-1, got %#v", store.insertedIDs)
	}
	profile := store.byID["8:user-1"]
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
	if perMessage["id"] != "8:user-1" || perMessage["displayname"] != "User One" {
		t.Fatalf("unexpected per-message profile: %#v", perMessage)
	}
}

func TestIngestThreadUsesCachedDisplayName(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "8:user-2", IMDisplayName: "", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	store := &fakeProfileStore{
		byID: map[string]*database.TeamsProfile{
			"8:user-2": {
				TeamsUserID: "8:user-2",
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
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

func TestIngestThreadUsesSendIntentMXIDForMessageMap(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", MessageID: "m1", ClientMessageID: "c1", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	sendIntents := &fakeSendIntentLookup{
		byClientMessageID: map[string]*database.TeamsSendIntent{
			"c1": {MXID: id.EventID("$original")},
		},
	}
	messageMap := &fakeMessageMapWriter{}
	ingestor := &MessageIngestor{
		Lister:      lister,
		Sender:      sender,
		SendIntents: sendIntents,
		MessageMap:  messageMap,
		Log:         zerolog.New(io.Discard),
	}

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(messageMap.entries) != 1 {
		t.Fatalf("expected one message map entry, got %d", len(messageMap.entries))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected matrix send to be skipped, got %#v", sender.sent)
	}
	entry := messageMap.entries[0]
	if entry.MXID != "$original" {
		t.Fatalf("expected original mxid mapping, got %s", entry.MXID)
	}
	if entry.ThreadID != "thread-1" || entry.TeamsMessageID != "msg/1" {
		t.Fatalf("unexpected mapping: %#v", entry)
	}
}

func TestIngestThreadStoresMessageMetadataInMap(t *testing.T) {
	ts := time.UnixMilli(1700000000123)
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", MessageID: "m1", SenderID: "8:user-2", Timestamp: ts, Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	messageMap := &fakeMessageMapWriter{}
	ingestor := &MessageIngestor{
		Lister:     lister,
		Sender:     sender,
		MessageMap: messageMap,
		Log:        zerolog.New(io.Discard),
	}

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(messageMap.entries) != 1 {
		t.Fatalf("expected one message map entry, got %d", len(messageMap.entries))
	}
	entry := messageMap.entries[0]
	if entry.MessageTS == nil || *entry.MessageTS != ts.UnixMilli() {
		t.Fatalf("unexpected message_ts: %#v", entry.MessageTS)
	}
	if entry.SenderID == nil || *entry.SenderID != "8:user-2" {
		t.Fatalf("unexpected sender_id: %#v", entry.SenderID)
	}
}

func TestIngestThreadSkipsSendButStillIngestsReactionsOnSendIntent(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", MessageID: "m1", ClientMessageID: "c1", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	sendIntents := &fakeSendIntentLookup{
		byClientMessageID: map[string]*database.TeamsSendIntent{
			"c1": {MXID: id.EventID("$original")},
		},
	}
	messageMap := &fakeMessageMapWriter{}
	reactions := &fakeReactionIngestor{}
	ingestor := &MessageIngestor{
		Lister:           lister,
		Sender:           sender,
		SendIntents:      sendIntents,
		MessageMap:       messageMap,
		ReactionIngestor: reactions,
		Log:              zerolog.New(io.Discard),
	}

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced || res.LastSequenceID != "1" {
		t.Fatalf("expected advancement on matched send intent, got %#v", res)
	}
	if res.MessagesIngested != 0 {
		t.Fatalf("expected 0 ingested messages when send is skipped, got %d", res.MessagesIngested)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("expected matrix send to be skipped, got %#v", sender.sent)
	}
	if len(messageMap.entries) != 1 || messageMap.entries[0].MXID != "$original" {
		t.Fatalf("expected message map to use original mxid, got %#v", messageMap.entries)
	}
	if len(reactions.targetMXIDs) != 1 || reactions.targetMXIDs[0] != "$original" {
		t.Fatalf("expected reactions to target original mxid, got %#v", reactions.targetMXIDs)
	}
}

func TestIngestThreadMarksUnreadForInboundMessages(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", MessageID: "m1", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	unread := &fakeUnreadTracker{}
	ingestor := &MessageIngestor{
		Lister:        lister,
		Sender:        sender,
		UnreadTracker: unread,
		Log:           zerolog.New(io.Discard),
	}

	_, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if len(unread.rooms) != 1 || unread.rooms[0] != "!room:example" {
		t.Fatalf("expected unread marker for room, got %#v", unread.rooms)
	}
}

func TestIngestThreadDoesNotMarkUnreadForSendIntentEcho(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", MessageID: "m1", ClientMessageID: "c1", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	unread := &fakeUnreadTracker{}
	sendIntents := &fakeSendIntentLookup{
		byClientMessageID: map[string]*database.TeamsSendIntent{
			"c1": {MXID: id.EventID("$original")},
		},
	}
	ingestor := &MessageIngestor{
		Lister:        lister,
		Sender:        sender,
		SendIntents:   sendIntents,
		UnreadTracker: unread,
		Log:           zerolog.New(io.Discard),
	}

	_, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if len(unread.rooms) != 0 {
		t.Fatalf("expected no unread marker for send intent echo, got %#v", unread.rooms)
	}
}

func TestIngestThreadUpdatesDisplayNameOnChange(t *testing.T) {
	when := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "8:user-3", IMDisplayName: "New Name", Timestamp: when, Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	store := &fakeProfileStore{
		byID: map[string]*database.TeamsProfile{
			"8:user-3": {
				TeamsUserID: "8:user-3",
				DisplayName: "Old Name",
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(store.updatedIDs) != 1 || store.updatedIDs[0] != "8:user-3" {
		t.Fatalf("expected display name update for user-3, got %#v", store.updatedIDs)
	}
	if store.byID["8:user-3"].DisplayName != "New Name" {
		t.Fatalf("expected updated display name, got %#v", store.byID["8:user-3"])
	}
	if !store.byID["8:user-3"].LastSeenTS.Equal(when) {
		t.Fatalf("expected last seen ts to update, got %s", store.byID["8:user-3"].LastSeenTS.Format(time.RFC3339))
	}
	perMessage, ok := sender.extra[0]["com.beeper.per_message_profile"].(map[string]any)
	if !ok {
		t.Fatalf("missing per-message profile metadata")
	}
	if perMessage["displayname"] != "New Name" {
		t.Fatalf("unexpected per-message display name: %#v", perMessage["displayname"])
	}
}

func TestIngestThreadDoesNotUpdateWhenIMDisplayNameEmpty(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "8:user-4", TokenDisplayName: "Token Name", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	store := &fakeProfileStore{
		byID: map[string]*database.TeamsProfile{
			"8:user-4": {
				TeamsUserID: "8:user-4",
				DisplayName: "",
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(store.updatedIDs) != 0 {
		t.Fatalf("expected no display name updates, got %#v", store.updatedIDs)
	}
	perMessage, ok := sender.extra[0]["com.beeper.per_message_profile"].(map[string]any)
	if !ok {
		t.Fatalf("missing per-message profile metadata")
	}
	if perMessage["displayname"] != "Token Name" {
		t.Fatalf("unexpected per-message display name: %#v", perMessage["displayname"])
	}
}

func TestIngestThreadDoesNotUpdateWhenNameUnchanged(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "8:user-5", IMDisplayName: "Same Name", Body: "one"},
		},
	}
	sender := &fakeMatrixSender{}
	store := &fakeProfileStore{
		byID: map[string]*database.TeamsProfile{
			"8:user-5": {
				TeamsUserID: "8:user-5",
				DisplayName: "Same Name",
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(store.updatedIDs) != 0 {
		t.Fatalf("expected no display name updates, got %#v", store.updatedIDs)
	}
	perMessage, ok := sender.extra[0]["com.beeper.per_message_profile"].(map[string]any)
	if !ok {
		t.Fatalf("missing per-message profile metadata")
	}
	if perMessage["displayname"] != "Same Name" {
		t.Fatalf("unexpected per-message display name: %#v", perMessage["displayname"])
	}
}

func TestIngestThreadSkipsProfileForNonUserID(t *testing.T) {
	lister := &fakeMessageLister{
		messages: []model.RemoteMessage{
			{SequenceID: "1", SenderID: "19:abc@thread.v2", IMDisplayName: "Thread Name", Body: "one"},
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

	res, err := ingestor.IngestThread(context.Background(), "thread-1", "@oneToOne.skype", "!room:example", nil)
	if err != nil {
		t.Fatalf("IngestThread failed: %v", err)
	}
	if !res.Advanced {
		t.Fatalf("expected advancement on success")
	}
	if len(store.insertedIDs) != 0 {
		t.Fatalf("expected no profile insert, got %#v", store.insertedIDs)
	}
	if len(store.updatedIDs) != 0 {
		t.Fatalf("expected no profile updates, got %#v", store.updatedIDs)
	}
}
