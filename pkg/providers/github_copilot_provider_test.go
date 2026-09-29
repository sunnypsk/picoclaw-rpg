package providers

import (
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	copilotrpc "github.com/github/copilot-sdk/go/rpc"
	"github.com/stretchr/testify/require"
)

func TestCopilotClientOptions(t *testing.T) {
	t.Setenv("COPILOT_SDK_DEFAULT_CONNECTION", "inprocess")
	local, ok := copilotClientOptions("").Connection.(copilot.StdioConnection)
	require.True(t, ok)
	require.Equal(t, "copilot", local.Path)
	connection, ok := copilotClientOptions("localhost:3000").Connection.(copilot.URIConnection)
	require.True(t, ok)
	require.Equal(t, "localhost:3000", connection.URL)
}

func TestCopilotSessionDeniesUnattendedPermissions(t *testing.T) {
	config := copilotSessionConfig("test-model")
	require.Equal(t, "test-model", config.Model)
	require.NotNil(t, config.OnPermissionRequest)
	decision, err := config.OnPermissionRequest(nil, copilot.PermissionInvocation{})
	require.NoError(t, err)
	require.IsType(t, &copilotrpc.PermissionDecisionUserNotAvailable{}, decision)
}

func TestParseCopilotResponse(t *testing.T) {
	response, err := parseCopilotResponse(&copilot.SessionEvent{
		Data: &copilot.AssistantMessageData{Content: "hello"},
	})
	require.NoError(t, err)
	require.Equal(t, "hello", response.Content)
	require.Equal(t, "stop", response.FinishReason)

	for _, event := range []*copilot.SessionEvent{
		nil,
		{},
		{Data: &copilot.SessionIdleData{}},
		{Data: (*copilot.AssistantMessageData)(nil)},
	} {
		_, err := parseCopilotResponse(event)
		require.Error(t, err)
	}
}
