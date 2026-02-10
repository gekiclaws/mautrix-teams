package teamsbridge

import (
	"strings"

	"go.mau.fi/mautrix-teams/internal/teams/model"
)

type ReactionKey struct {
	ThreadID       string
	TeamsMessageID string
	TeamsUserID    string
	ReactionKey    string
}

func NewReactionKey(threadID, teamsMessageID, teamsUserID, reactionKey string) (ReactionKey, bool) {
	threadID = strings.TrimSpace(threadID)
	teamsMessageID = NormalizeTeamsReactionMessageID(teamsMessageID)
	teamsUserID = model.NormalizeTeamsUserID(teamsUserID)
	reactionKey = strings.TrimSpace(reactionKey)
	if threadID == "" || teamsMessageID == "" || teamsUserID == "" || reactionKey == "" {
		return ReactionKey{}, false
	}
	return ReactionKey{
		ThreadID:       threadID,
		TeamsMessageID: teamsMessageID,
		TeamsUserID:    teamsUserID,
		ReactionKey:    reactionKey,
	}, true
}
