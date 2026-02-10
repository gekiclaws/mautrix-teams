package main

import (
	"errors"
	"strings"

	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

const teamsVirtualUserLocalpartPrefix = "sh-msteams_"

type TeamsVirtualUserReadReceiptSender struct {
	Bridge *TeamsBridge
}

func (s *TeamsVirtualUserReadReceiptSender) SetReadMarkers(roomID id.RoomID, eventID id.EventID, teamsUserID string) error {
	if s == nil || s.Bridge == nil {
		return errors.New("missing bridge")
	}
	intent, err := s.Bridge.intentForTeamsVirtualUser(teamsUserID)
	if err != nil {
		return err
	}
	if err = intent.EnsureJoined(roomID); err != nil {
		return err
	}
	return intent.SendReceipt(roomID, eventID, event.ReceiptTypeRead, map[string]any{})
}

func (br *TeamsBridge) intentForTeamsVirtualUser(teamsUserID string) (*appservice.IntentAPI, error) {
	if br == nil || br.Config == nil || br.AS == nil {
		return nil, errors.New("missing bridge appservice context")
	}
	mxid := br.mxidForTeamsVirtualUser(teamsUserID)
	if mxid == "" {
		return nil, errors.New("missing teams virtual user mxid")
	}
	intent := br.AS.Intent(mxid)
	if intent == nil {
		return nil, errors.New("missing teams virtual user intent")
	}
	return intent, nil
}

func (br *TeamsBridge) mxidForTeamsVirtualUser(teamsUserID string) id.UserID {
	if br == nil || br.Config == nil {
		return ""
	}
	normalized := model.NormalizeTeamsUserID(teamsUserID)
	if normalized == "" {
		return ""
	}
	localpart := teamsVirtualUserLocalpartPrefix + id.EncodeUserLocalpart(normalized)
	if localpart == "" {
		return ""
	}
	if err := id.ValidateUserLocalpart(localpart); err != nil {
		return ""
	}
	homeserver := strings.TrimSpace(br.Config.Homeserver.Domain)
	if homeserver == "" {
		return ""
	}
	return id.NewUserID(localpart, homeserver)
}
