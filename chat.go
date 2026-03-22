package dashboard

import (
	"context"
	"net/http"

	appchat "github.com/mudrii/openclaw-dashboard/internal/appchat"
)

type chatMessage = appchat.Message
type chatRequest = appchat.Request
type completionPayload = appchat.CompletionPayload
type gatewayError = appchat.GatewayError

func buildSystemPrompt(data map[string]any) string {
	return appchat.BuildSystemPrompt(data)
}

func callGateway(ctx context.Context, system string, history []chatMessage, question string, port int, token, model string, client *http.Client) (string, error) {
	return appchat.CallGateway(ctx, system, history, question, port, token, model, client)
}
