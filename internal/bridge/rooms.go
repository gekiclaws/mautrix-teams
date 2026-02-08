package teamsbridge

import (
	"context"
	"errors"
	"sort"
	"strings"

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

type RoomAdminInviter interface {
	EnsureInvited(roomID id.RoomID, userID id.UserID) (string, error)
}

type IntentAdminInviter struct {
	Intent *appservice.IntentAPI
}

func NewIntentAdminInviter(intent *appservice.IntentAPI) *IntentAdminInviter {
	return &IntentAdminInviter{Intent: intent}
}

func (i *IntentAdminInviter) EnsureInvited(roomID id.RoomID, userID id.UserID) (string, error) {
	if i == nil || i.Intent == nil {
		return "", errors.New("missing bot intent")
	}
	if i.Intent.StateStore != nil {
		if i.Intent.StateStore.IsInRoom(roomID, userID) {
			return "already_joined", nil
		}
		if i.Intent.StateStore.IsInvited(roomID, userID) {
			return "already_invited", nil
		}
	}

	_, err := i.Intent.InviteUser(roomID, &mautrix.ReqInviteUser{UserID: userID})
	if err == nil {
		if i.Intent.StateStore != nil {
			i.Intent.StateStore.SetMembership(roomID, userID, event.MembershipInvite)
		}
		return "invited", nil
	}

	var httpErr mautrix.HTTPError
	if errors.As(err, &httpErr) && httpErr.RespError != nil {
		lowerErr := strings.ToLower(httpErr.RespError.Err)
		if strings.Contains(lowerErr, "already in the room") {
			if i.Intent.StateStore != nil {
				i.Intent.StateStore.SetMembership(roomID, userID, event.MembershipJoin)
			}
			return "already_joined", nil
		}
		if strings.Contains(lowerErr, "already invited") || strings.Contains(lowerErr, "is invited") {
			if i.Intent.StateStore != nil {
				i.Intent.StateStore.SetMembership(roomID, userID, event.MembershipInvite)
			}
			return "already_invited", nil
		}
	}

	return "", err
}

func ResolveExplicitAdminMXIDs(permissions bridgeconfig.PermissionConfig) []id.UserID {
	if len(permissions) == 0 {
		return nil
	}
	mxids := make([]id.UserID, 0, len(permissions))
	for rawKey, level := range permissions {
		if level < bridgeconfig.PermissionLevelAdmin {
			continue
		}
		key := strings.TrimSpace(rawKey)
		if !strings.HasPrefix(key, "@") {
			continue
		}
		mxid := id.UserID(key)
		if _, _, err := mxid.Parse(); err != nil {
			continue
		}
		mxids = append(mxids, mxid)
	}
	sort.Slice(mxids, func(i, j int) bool {
		return mxids[i] < mxids[j]
	})
	return mxids
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
	Store        ThreadStore
	Creator      RoomCreator
	AdminInviter RoomAdminInviter
	AdminMXIDs   []id.UserID
	Log          zerolog.Logger
}

func NewRoomsService(store ThreadStore, creator RoomCreator, adminInviter RoomAdminInviter, adminMXIDs []id.UserID, log zerolog.Logger) *RoomsService {
	return &RoomsService{
		Store:        store,
		Creator:      creator,
		AdminInviter: adminInviter,
		AdminMXIDs:   adminMXIDs,
		Log:          log,
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
			r.ensureAdminsInvited(thread.ID, roomID)
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
	r.ensureAdminsInvited(thread.ID, roomID)
	return roomID, true, nil
}

func (r *RoomsService) ensureAdminsInvited(threadID string, roomID id.RoomID) {
	if r == nil || r.AdminInviter == nil || len(r.AdminMXIDs) == 0 || roomID == "" {
		return
	}
	for _, adminMXID := range r.AdminMXIDs {
		result, err := r.AdminInviter.EnsureInvited(roomID, adminMXID)
		log := r.Log.Debug().
			Str("room_id", roomID.String()).
			Str("thread_id", threadID).
			Str("admin_mxid", adminMXID.String())
		if err != nil {
			log.Err(err).Str("result", "invite_failed").Msg("matrix admin invite ensure failed")
			continue
		}
		log.Str("result", result).Msg("matrix admin invite ensured")
	}
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
