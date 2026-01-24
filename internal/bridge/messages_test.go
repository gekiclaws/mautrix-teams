package teamsbridge

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"

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
}

func (f *fakeMatrixSender) SendText(roomID id.RoomID, body string) (id.EventID, error) {
	if body == f.failBody {
		return "", errors.New("send failed")
	}
	f.sent = append(f.sent, body)
	return id.EventID("$event"), nil
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
