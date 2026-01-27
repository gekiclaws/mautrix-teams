package teamsbridge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge/bridgeconfig"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type RoomCreator interface {
	CreateRoom(thread model.Thread) (id.RoomID, error)
}

type ClientRoomCreator struct {
	Client      *mautrix.Client
	Encryption  bridgeconfig.EncryptionConfig
	RoomVersion string
}

func NewClientRoomCreator(client *mautrix.Client, cfg bridgeconfig.BridgeConfig) *ClientRoomCreator {
	return &ClientRoomCreator{
		Client:      client,
		Encryption:  cfg.GetEncryptionConfig(),
		RoomVersion: "11",
	}
}

func (c *ClientRoomCreator) CreateRoom(thread model.Thread) (id.RoomID, error) {
	if c.Client == nil {
		return "", errors.New("missing matrix client")
	}

	initialState := []*event.Event{}
	if c.Encryption.Default {
		initialState = append(initialState, &event.Event{
			Type: event.StateEncryption,
			Content: event.Content{
				Parsed: encryptionEventContent(c.Encryption),
			},
		})
	}

	name := ""
	if thread.IsOneToOne {
		name = "Chat"
	}

	req := &mautrix.ReqCreateRoom{
		Visibility:      "private",
		Name:            name,
		Preset:          presetForThread(thread),
		IsDirect:        thread.IsOneToOne,
		InitialState:    initialState,
		CreationContent: map[string]interface{}{"m.federate": false},
		RoomVersion:     c.RoomVersion,
	}

	resp, err := c.Client.CreateRoom(req)
	if err != nil {
		return "", err
	}
	return resp.RoomID, nil
}

type IntentRoomCreator struct {
	Intent      *appservice.IntentAPI
	Encryption  bridgeconfig.EncryptionConfig
	RoomVersion string
}

func NewIntentRoomCreator(intent *appservice.IntentAPI, cfg bridgeconfig.BridgeConfig) *IntentRoomCreator {
	return &IntentRoomCreator{
		Intent:      intent,
		Encryption:  cfg.GetEncryptionConfig(),
		RoomVersion: "11",
	}
}

func (c *IntentRoomCreator) CreateRoom(thread model.Thread) (id.RoomID, error) {
	if c.Intent == nil {
		return "", errors.New("missing bot intent")
	}
	if err := c.Intent.EnsureRegistered(); err != nil {
		return "", err
	}

	initialState := []*event.Event{}
	if c.Encryption.Default {
		initialState = append(initialState, &event.Event{
			Type: event.StateEncryption,
			Content: event.Content{
				Parsed: encryptionEventContent(c.Encryption),
			},
		})
	}

	name := ""
	if thread.IsOneToOne {
		name = "Chat"
	}

	req := &mautrix.ReqCreateRoom{
		Visibility:      "private",
		Name:            name,
		Preset:          presetForThread(thread),
		IsDirect:        thread.IsOneToOne,
		InitialState:    initialState,
		CreationContent: map[string]interface{}{"m.federate": false},
		RoomVersion:     c.RoomVersion,
	}

	resp, err := c.Intent.CreateRoom(req)
	if err != nil {
		return "", err
	}
	return resp.RoomID, nil
}

func presetForThread(thread model.Thread) string {
	if thread.IsOneToOne {
		return "private_chat"
	}
	return "private"
}

func encryptionEventContent(cfg bridgeconfig.EncryptionConfig) *event.EncryptionEventContent {
	evt := &event.EncryptionEventContent{Algorithm: id.AlgorithmMegolmV1}
	if cfg.Rotation.EnableCustom {
		evt.RotationPeriodMillis = cfg.Rotation.Milliseconds
		evt.RotationPeriodMessages = cfg.Rotation.Messages
	}
	return evt
}

type RoomsService struct {
	Store   ThreadStore
	Creator RoomCreator
	Log     zerolog.Logger
}

func NewRoomsService(store ThreadStore, creator RoomCreator, log zerolog.Logger) *RoomsService {
	return &RoomsService{
		Store:   store,
		Creator: creator,
		Log:     log,
	}
}

func (r *RoomsService) EnsureRoom(thread model.Thread) (id.RoomID, bool, error) {
	if r.Store != nil {
		if roomID, ok := r.Store.Get(thread.ID); ok {
			r.Log.Debug().
				Str("thread_id", thread.ID).
				Str("room_id", roomID.String()).
				Msg("matrix room exists")
			if err := r.Store.Put(thread, roomID); err != nil {
				return "", false, err
			}
			return roomID, false, nil
		}
	}
	if r.Creator == nil {
		return "", false, errors.New("missing room creator")
	}
	roomID, err := r.Creator.CreateRoom(thread)
	if err != nil {
		return "", false, err
	}
	if r.Store != nil {
		if err := r.Store.Put(thread, roomID); err != nil {
			return "", false, err
		}
	}
	r.Log.Info().
		Str("room_id", roomID.String()).
		Str("thread_id", thread.ID).
		Msg("matrix room created")
	return roomID, true, nil
}

type ConversationLister interface {
	ListConversations(ctx context.Context, token string) ([]model.RemoteConversation, error)
}

func DiscoverAndEnsureRooms(ctx context.Context, token string, lister ConversationLister, rooms *RoomsService, log zerolog.Logger) error {
	discoverer := &TeamsThreadDiscoverer{
		Lister: lister,
		Token:  token,
		Log:    log,
	}
	threads, err := discoverer.Discover(ctx)
	if err != nil {
		return err
	}

	for _, thread := range threads {
		log.Info().
			Str("thread_id", thread.ID).
			Str("type", thread.Type).
			Msg("teams thread discovered")
		if rooms == nil {
			continue
		}
		if _, _, err := rooms.EnsureRoom(thread); err != nil {
			return err
		}
	}
	return nil
}
