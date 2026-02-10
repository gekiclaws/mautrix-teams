package main

import (
	"errors"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type TeamsVirtualUserReactionSender struct {
	Bridge *TeamsBridge
}

func (s *TeamsVirtualUserReactionSender) SendReactionAsTeamsUser(roomID id.RoomID, target id.EventID, key string, teamsUserID string) (id.EventID, error) {
	if s == nil || s.Bridge == nil {
		return "", errors.New("missing bridge")
	}
	if roomID == "" || target == "" || key == "" {
		return "", errors.New("missing reaction send args")
	}
	intent, err := s.Bridge.intentForTeamsVirtualUser(teamsUserID)
	if err != nil {
		return "", err
	}
	if err = intent.EnsureJoined(roomID); err != nil {
		return "", err
	}
	if err = s.Bridge.ensureTeamsVirtualUserMemberProfile(intent, roomID, teamsUserID); err != nil {
		return "", err
	}
	content := event.ReactionEventContent{
		RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: target,
			Key:     key,
		},
	}
	wrapped := event.Content{
		Parsed: &content,
		Raw: map[string]any{
			"com.beeper.teams.ingested_reaction": true,
		},
	}
	resp, err := intent.SendMessageEvent(roomID, event.EventReaction, &wrapped)
	if err != nil {
		return "", err
	}
	return resp.EventID, nil
}

func (s *TeamsVirtualUserReactionSender) RedactReactionAsTeamsUser(roomID id.RoomID, eventID id.EventID, teamsUserID string) error {
	if s == nil || s.Bridge == nil {
		return errors.New("missing bridge")
	}
	if roomID == "" || eventID == "" {
		return errors.New("missing reaction redact args")
	}
	intent, err := s.Bridge.intentForTeamsVirtualUser(teamsUserID)
	if err != nil {
		return err
	}
	if err = intent.EnsureJoined(roomID); err != nil {
		return err
	}
	if err = s.Bridge.ensureTeamsVirtualUserMemberProfile(intent, roomID, teamsUserID); err != nil {
		return err
	}
	_, err = intent.RedactEvent(roomID, eventID)
	return err
}
