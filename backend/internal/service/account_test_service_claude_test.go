//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateTestPayload_UsesFingerprintUAAndClaudeCodeShape(t *testing.T) {
	payload, err := createTestPayload("claude-sonnet-4-5-20250929", "claude-cli/2.1.84 (external, cli)")
	require.NoError(t, err)

	require.Equal(t, "claude-sonnet-4-5-20250929", payload["model"])
	require.Equal(t, 16384, payload["max_tokens"])
	require.Equal(t, true, payload["stream"])

	_, hasTemperature := payload["temperature"]
	require.False(t, hasTemperature, "temperature should not be sent by Claude Code test payload")

	tools, ok := payload["tools"].([]any)
	require.True(t, ok, "tools should be present")
	require.Len(t, tools, 0, "tools should be an explicit empty array")

	metadata, ok := payload["metadata"].(map[string]string)
	require.True(t, ok, "metadata should be present")
	require.Contains(t, metadata["user_id"], `"device_id"`)
	require.Contains(t, metadata["user_id"], `"session_id"`)
}
