package model

import "strings"

type ThreadProperties struct {
	OriginalThreadID  string `json:"originalThreadId"`
	ProductThreadType string `json:"productThreadType"`
	CreatedAt         string `json:"createdat"`
	IsCreator         bool   `json:"isCreator"`
}

type RemoteConversation struct {
	ThreadProperties ThreadProperties `json:"threadProperties"`
}

type Thread struct {
	ID           string
	Type         string
	CreatedAtRaw string
	IsCreator    bool
	IsOneToOne   bool
}

func (c RemoteConversation) Normalize() (Thread, bool) {
	id := strings.TrimSpace(c.ThreadProperties.OriginalThreadID)
	if id == "" {
		return Thread{}, false
	}
	threadType := strings.TrimSpace(c.ThreadProperties.ProductThreadType)
	return Thread{
		ID:           id,
		Type:         threadType,
		CreatedAtRaw: c.ThreadProperties.CreatedAt,
		IsCreator:    c.ThreadProperties.IsCreator,
		IsOneToOne:   threadType == "OneToOneChat",
	}, true
}
