package connector

import (
	"context"
	"errors"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
)

func (c *TeamsClient) HandleMatrixTyping(ctx context.Context, msg *bridgev2.MatrixTyping) error {
	if !c.IsLoggedIn() {
		return bridgev2.ErrNotLoggedIn
	}
	if err := c.ensureValidSkypeToken(ctx); err != nil {
		return err
	}
	if msg == nil || !msg.IsTyping {
		return nil
	}
	threadID := strings.TrimSpace(string(msg.Portal.ID))
	if threadID == "" {
		return errors.New("missing thread id")
	}
	consumer := c.getConsumer()
	if consumer == nil {
		return errors.New("missing consumer client")
	}
	consumer.Token = c.Meta.SkypeToken
	_, err := consumer.SendTypingIndicator(ctx, threadID, c.Meta.TeamsUserID)
	return err
}
