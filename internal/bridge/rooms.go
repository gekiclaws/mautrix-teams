package teamsbridge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

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

type RoomStateReconciler interface {
	EnsureRoomState(roomID id.RoomID, adminMXIDs []id.UserID) (historyResult string, powerResult string, err error)
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

	initialMembership, err := i.getMembership(roomID, userID)
	if err != nil {
		return "", err
	}
	if initialMembership == event.MembershipJoin {
		return "already_joined", nil
	}

	inviteResult := ""
	switch initialMembership {
	case event.MembershipInvite:
		// Retry invite for pending invites to recover from resync storms where auto-join is delayed.
		inviteResult, err = i.sendInvite(roomID, userID)
		if err != nil {
			return "", err
		}
	default:
		inviteResult, err = i.sendInvite(roomID, userID)
		if err != nil {
			return "", err
		}
	}

	finalMembership, err := i.getMembership(roomID, userID)
	if err != nil {
		return inviteResult, err
	}
	switch finalMembership {
	case event.MembershipJoin:
		if inviteResult == "already_joined" || inviteResult == "" {
			return "already_joined", nil
		}
		return "joined_after_invite", nil
	case event.MembershipInvite:
		return "invite_pending", nil
	default:
		if inviteResult == "already_invited" || inviteResult == "invited" {
			return "invite_pending", nil
		}
	}

	return inviteResult, nil
}

func (i *IntentAdminInviter) sendInvite(roomID id.RoomID, userID id.UserID) (string, error) {
	content := event.Content{
		Parsed: &event.MemberEventContent{
			Membership: event.MembershipInvite,
		},
		Raw: map[string]interface{}{
			"fi.mau.will_auto_accept": true,
		},
	}
	_, err := i.Intent.SendStateEvent(roomID, event.StateMember, userID.String(), &content)
	if err == nil {
		if i.Intent.StateStore != nil {
			i.Intent.StateStore.SetMembership(roomID, userID, event.MembershipInvite)
		}
		return "invited", nil
	}

	var httpErr mautrix.HTTPError
	if errors.As(err, &httpErr) && httpErr.RespError != nil {
		lowerErr := strings.ToLower(httpErr.RespError.Err)
		if strings.Contains(lowerErr, "already in the room") || strings.Contains(lowerErr, "is already joined") {
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

func (i *IntentAdminInviter) getMembership(roomID id.RoomID, userID id.UserID) (event.Membership, error) {
	if i.Intent.StateStore != nil {
		if i.Intent.StateStore.IsInRoom(roomID, userID) {
			return event.MembershipJoin, nil
		}
	}

	var member event.MemberEventContent
	err := i.Intent.StateEvent(roomID, event.StateMember, userID.String(), &member)
	if err != nil {
		if errors.Is(err, mautrix.MNotFound) {
			return event.MembershipLeave, nil
		}
		return "", err
	}

	if i.Intent.StateStore != nil {
		i.Intent.StateStore.SetMembership(roomID, userID, member.Membership)
	}
	return member.Membership, nil
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
	AdminMXIDs  []id.UserID
}

func NewClientRoomCreator(client *mautrix.Client, cfg bridgeconfig.BridgeConfig, adminMXIDs []id.UserID) *ClientRoomCreator {
	return &ClientRoomCreator{
		Client:      client,
		Encryption:  cfg.GetEncryptionConfig(),
		RoomVersion: "11",
		AdminMXIDs:  adminMXIDs,
	}
}

func (c *ClientRoomCreator) CreateRoom(thread model.Thread) (id.RoomID, error) {
	if c.Client == nil {
		return "", errors.New("missing matrix client")
	}

	initialState := roomInitialState(c.Encryption, c.Client.UserID, c.AdminMXIDs)

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
	AdminMXIDs  []id.UserID
}

func NewIntentRoomCreator(intent *appservice.IntentAPI, cfg bridgeconfig.BridgeConfig, adminMXIDs []id.UserID) *IntentRoomCreator {
	return &IntentRoomCreator{
		Intent:      intent,
		Encryption:  cfg.GetEncryptionConfig(),
		RoomVersion: "11",
		AdminMXIDs:  adminMXIDs,
	}
}

func (c *IntentRoomCreator) CreateRoom(thread model.Thread) (id.RoomID, error) {
	if c.Intent == nil {
		return "", errors.New("missing bot intent")
	}
	if err := c.Intent.EnsureRegistered(); err != nil {
		return "", err
	}

	initialState := roomInitialState(c.Encryption, c.Intent.UserID, c.AdminMXIDs)

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

func roomInitialState(cfg bridgeconfig.EncryptionConfig, creatorMXID id.UserID, adminMXIDs []id.UserID) []*event.Event {
	initialState := []*event.Event{
		{
			Type: event.StateHistoryVisibility,
			Content: event.Content{
				Parsed: &event.HistoryVisibilityEventContent{
					HistoryVisibility: event.HistoryVisibilityShared,
				},
			},
		},
		{
			Type: event.StatePowerLevels,
			Content: event.Content{
				Parsed: roomInitialPowerLevels(creatorMXID, adminMXIDs),
			},
		},
	}
	if cfg.Default {
		initialState = append(initialState, &event.Event{
			Type: event.StateEncryption,
			Content: event.Content{
				Parsed: encryptionEventContent(cfg),
			},
		})
	}
	return initialState
}

func roomInitialPowerLevels(creatorMXID id.UserID, adminMXIDs []id.UserID) *event.PowerLevelsEventContent {
	users := make(map[id.UserID]int, len(adminMXIDs)+1)
	if creatorMXID != "" {
		users[creatorMXID] = 100
	}
	for _, adminMXID := range adminMXIDs {
		if _, ok := users[adminMXID]; !ok {
			users[adminMXID] = 0
		}
	}
	return &event.PowerLevelsEventContent{
		Users: users,
		Events: map[string]int{
			event.EventMessage.String(): 0,
		},
	}
}

type IntentRoomStateReconciler struct {
	Intent *appservice.IntentAPI
}

func NewIntentRoomStateReconciler(intent *appservice.IntentAPI) *IntentRoomStateReconciler {
	return &IntentRoomStateReconciler{Intent: intent}
}

func (r *IntentRoomStateReconciler) EnsureRoomState(roomID id.RoomID, adminMXIDs []id.UserID) (string, string, error) {
	if r == nil || r.Intent == nil {
		return "", "", errors.New("missing bot intent")
	}

	historyResult := "already_shared"
	var errs []error

	var historyVisibility event.HistoryVisibilityEventContent
	historyErr := r.Intent.StateEvent(roomID, event.StateHistoryVisibility, "", &historyVisibility)
	needsHistoryUpdate := false
	if historyErr != nil {
		if errors.Is(historyErr, mautrix.MNotFound) {
			needsHistoryUpdate = true
		} else {
			errs = append(errs, fmt.Errorf("get history visibility: %w", historyErr))
		}
	} else if historyVisibility.HistoryVisibility != event.HistoryVisibilityShared {
		needsHistoryUpdate = true
	}
	if needsHistoryUpdate {
		_, setErr := r.Intent.SendStateEvent(roomID, event.StateHistoryVisibility, "", &event.Content{
			Parsed: &event.HistoryVisibilityEventContent{
				HistoryVisibility: event.HistoryVisibilityShared,
			},
		})
		if setErr != nil {
			errs = append(errs, fmt.Errorf("set history visibility: %w", setErr))
		} else {
			historyResult = "set_shared"
		}
	}

	powerResult := "no_admins"
	if len(adminMXIDs) > 0 {
		pl, plErr := r.Intent.PowerLevels(roomID)
		if plErr != nil {
			errs = append(errs, fmt.Errorf("get power levels: %w", plErr))
		} else {
			changed := ensureAdminSendPermissionLevels(pl, adminMXIDs)
			if changed {
				_, setErr := r.Intent.SetPowerLevels(roomID, pl)
				if setErr != nil {
					errs = append(errs, fmt.Errorf("set power levels: %w", setErr))
					powerResult = "set_failed"
				} else {
					powerResult = "raised_admin_send_level"
				}
			} else {
				powerResult = "already_sufficient"
			}
		}
	}

	return historyResult, powerResult, errors.Join(errs...)
}

func ensureAdminSendPermissionLevels(pl *event.PowerLevelsEventContent, adminMXIDs []id.UserID) bool {
	if pl == nil || len(adminMXIDs) == 0 {
		return false
	}
	if pl.Users == nil {
		pl.Users = make(map[id.UserID]int)
	}
	if pl.Events == nil {
		pl.Events = make(map[string]int)
	}
	requiredLevel := pl.GetEventLevel(event.EventMessage)
	changed := false
	for _, adminMXID := range adminMXIDs {
		if pl.GetUserLevel(adminMXID) < requiredLevel {
			pl.SetUserLevel(adminMXID, requiredLevel)
			changed = true
		}
	}
	return changed
}

type RoomsService struct {
	Store        ThreadStore
	Creator      RoomCreator
	AdminInviter RoomAdminInviter
	Reconciler   RoomStateReconciler
	AdminMXIDs   []id.UserID
	Log          zerolog.Logger

	claimMu      sync.Mutex
	threadClaims map[string]*sync.Mutex
}

func NewRoomsService(store ThreadStore, creator RoomCreator, adminInviter RoomAdminInviter, reconciler RoomStateReconciler, adminMXIDs []id.UserID, log zerolog.Logger) *RoomsService {
	return &RoomsService{
		Store:        store,
		Creator:      creator,
		AdminInviter: adminInviter,
		Reconciler:   reconciler,
		AdminMXIDs:   adminMXIDs,
		Log:          log,
		threadClaims: make(map[string]*sync.Mutex),
	}
}

func (r *RoomsService) EnsureRoom(thread model.Thread) (id.RoomID, bool, error) {
	if r == nil {
		return "", false, errors.New("missing rooms service")
	}
	claim := r.claimThread(thread.ID)
	claim.Lock()
	defer claim.Unlock()

	if r.Store != nil {
		if roomID, ok := r.Store.Get(thread.ID); ok {
			r.Log.Debug().
				Str("thread_id", thread.ID).
				Str("room_id", roomID.String()).
				Msg("matrix room exists")
			if err := r.Store.Put(thread, roomID); err != nil {
				return "", false, err
			}
			r.ensureRoomState(thread.ID, roomID)
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
	r.ensureRoomState(thread.ID, roomID)
	r.ensureAdminsInvited(thread.ID, roomID)
	return roomID, true, nil
}

func (r *RoomsService) claimThread(threadID string) *sync.Mutex {
	r.claimMu.Lock()
	defer r.claimMu.Unlock()
	claim, ok := r.threadClaims[threadID]
	if ok {
		return claim
	}
	claim = &sync.Mutex{}
	r.threadClaims[threadID] = claim
	return claim
}

func (r *RoomsService) ensureRoomState(threadID string, roomID id.RoomID) {
	if r == nil || r.Reconciler == nil || roomID == "" {
		return
	}
	historyResult, powerResult, err := r.Reconciler.EnsureRoomState(roomID, r.AdminMXIDs)
	log := r.Log.Debug().
		Str("room_id", roomID.String()).
		Str("thread_id", threadID).
		Str("history_result", historyResult).
		Str("power_result", powerResult)
	if err != nil {
		log.Err(err).Msg("matrix room state ensure failed")
		return
	}
	log.Msg("matrix room state ensured")
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
		if result == "already_joined" || result == "joined_after_invite" {
			log.Str("result", result).Msg("matrix admin membership ensured")
			continue
		}
		r.Log.Warn().
			Str("room_id", roomID.String()).
			Str("thread_id", threadID).
			Str("admin_mxid", adminMXID.String()).
			Str("result", result).
			Msg("matrix admin is not joined after membership reconciliation")
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
