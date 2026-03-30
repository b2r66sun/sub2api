package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBillingBlock_VersionMismatch(t *testing.T) {
	body := buildBodyWithBilling("x-anthropic-billing-header: cc_version=2.1.80.a3f; cc_entrypoint=cli; cch=00000;")
	result := NormalizeBillingBlock(body, "claude-cli/2.1.86 (external, cli)")

	text := extractSystemText(t, result, 0)
	require.Contains(t, text, "cc_version=2.1.86.a3f")
	require.Contains(t, text, "cc_entrypoint=cli")
	require.Contains(t, text, "cch=00000")
}

func TestNormalizeBillingBlock_AlreadyMatching(t *testing.T) {
	body := buildBodyWithBilling("x-anthropic-billing-header: cc_version=2.1.86.d9e; cc_entrypoint=cli; cch=00000;")
	result := NormalizeBillingBlock(body, "claude-cli/2.1.86 (external, cli)")

	require.Equal(t, string(body), string(result), "body should be unchanged when version already matches")
}

func TestNormalizeBillingBlock_NotBillingBlock(t *testing.T) {
	body := buildBodyWithBilling("You are Claude Code, Anthropic's official CLI for Claude.")
	result := NormalizeBillingBlock(body, "claude-cli/2.1.86 (external, cli)")

	require.Equal(t, string(body), string(result), "body should be unchanged when system[0] is not a billing block")
}

func TestNormalizeBillingBlock_NoSystemArray(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5-20250929","messages":[]}`)
	result := NormalizeBillingBlock(body, "claude-cli/2.1.86 (external, cli)")

	require.Equal(t, string(body), string(result))
}

func TestNormalizeBillingBlock_EmptyInputs(t *testing.T) {
	body := buildBodyWithBilling("x-anthropic-billing-header: cc_version=2.1.80.a3f; cc_entrypoint=cli; cch=00000;")

	require.Equal(t, string(body), string(NormalizeBillingBlock(body, "")), "empty UA")
	require.Nil(t, NormalizeBillingBlock(nil, "claude-cli/2.1.86"), "nil body")
	require.Equal(t, 0, len(NormalizeBillingBlock([]byte{}, "claude-cli/2.1.86")), "empty body")
}

func TestNormalizeBillingBlock_PreservesSuffix(t *testing.T) {
	body := buildBodyWithBilling("x-anthropic-billing-header: cc_version=2.1.80.88c; cc_entrypoint=cli; cch=00000;")
	result := NormalizeBillingBlock(body, "claude-cli/2.1.90 (external, cli)")

	text := extractSystemText(t, result, 0)
	require.Contains(t, text, "cc_version=2.1.90.88c", "suffix .88c should be preserved")
}

func TestNormalizeBillingBlock_NoSuffix(t *testing.T) {
	// Edge case: cc_version without a .suffix
	body := buildBodyWithBilling("x-anthropic-billing-header: cc_version=2.1.80; cc_entrypoint=cli; cch=00000;")
	result := NormalizeBillingBlock(body, "claude-cli/2.1.86 (external, cli)")

	text := extractSystemText(t, result, 0)
	require.Contains(t, text, "cc_version=2.1.86;", "version should be replaced even without suffix")
}

func TestNormalizeBillingBlock_NonCLIUserAgent(t *testing.T) {
	body := buildBodyWithBilling("x-anthropic-billing-header: cc_version=2.1.80.a3f; cc_entrypoint=cli; cch=00000;")
	result := NormalizeBillingBlock(body, "Mozilla/5.0")

	require.Equal(t, string(body), string(result), "non-CLI UA should not trigger normalization")
}

func TestGenerateBillingBlockText_Format(t *testing.T) {
	text := GenerateBillingBlockText("2.1.86")

	require.True(t, strings.HasPrefix(text, "x-anthropic-billing-header:"))
	require.Contains(t, text, "cc_version=2.1.86.")
	require.Contains(t, text, "cc_entrypoint=cli")
	require.Contains(t, text, "cch=00000")
}

func TestGenerateBillingBlockText_SuffixLength(t *testing.T) {
	// Generate multiple times to verify suffix is always 3 chars
	for i := 0; i < 10; i++ {
		text := GenerateBillingBlockText("2.1.86")
		// Extract suffix: between "2.1.86." and ";"
		idx := strings.Index(text, "2.1.86.")
		require.NotEqual(t, -1, idx)
		after := text[idx+len("2.1.86."):]
		semi := strings.Index(after, ";")
		require.NotEqual(t, -1, semi)
		suffix := after[:semi]
		require.Len(t, suffix, 3, "suffix should be 3 hex chars")
	}
}

func TestExtractBillingBlockVersion(t *testing.T) {
	tests := []struct {
		name    string
		billing string
		want    string
	}{
		{"normal", "x-anthropic-billing-header: cc_version=2.1.86.88c; cc_entrypoint=cli; cch=00000;", "2.1.86"},
		{"no suffix", "x-anthropic-billing-header: cc_version=2.1.84; cc_entrypoint=cli; cch=00000;", "2.1.84"},
		{"not billing", "You are Claude Code.", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := buildBodyWithBilling(tt.billing)
			got := ExtractBillingBlockVersion(body)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExtractBillingBlockVersion_NoSystem(t *testing.T) {
	body := []byte(`{"model":"test","messages":[]}`)
	require.Equal(t, "", ExtractBillingBlockVersion(body))
	require.Equal(t, "", ExtractBillingBlockVersion(nil))
	require.Equal(t, "", ExtractBillingBlockVersion([]byte{}))
}

func TestReconstructUAVersion(t *testing.T) {
	tests := []struct {
		ua, newVer, want string
	}{
		{"claude-cli/2.1.84 (external, cli)", "2.1.86", "claude-cli/2.1.86 (external, cli)"},
		{"claude-cli/2.1.84", "2.1.90", "claude-cli/2.1.90"},
		{"claude-cli/1.0.0 (darwin; arm64)", "2.1.86", "claude-cli/2.1.86 (darwin; arm64)"},
		// Must NOT replace Node.js runtime version, only the first (claude-cli) version
		{"claude-cli/2.1.84 Node.js/22.13.1 linux/x64", "2.1.86", "claude-cli/2.1.86 Node.js/22.13.1 linux/x64"},
		{"", "2.1.86", ""},
		{"claude-cli/2.1.84", "", "claude-cli/2.1.84"},
	}
	for _, tt := range tests {
		t.Run(tt.ua+"→"+tt.newVer, func(t *testing.T) {
			got := ReconstructUAVersion(tt.ua, tt.newVer)
			require.Equal(t, tt.want, got)
		})
	}
}

// --- helpers ---

func buildBodyWithBilling(systemText string) []byte {
	body := map[string]any{
		"model": "claude-sonnet-4-5-20250929",
		"system": []map[string]any{
			{"type": "text", "text": systemText},
			{"type": "text", "text": "You are Claude Code."},
		},
		"messages": []any{},
	}
	b, _ := json.Marshal(body)
	return b
}

func extractSystemText(t *testing.T, body []byte, index int) string {
	t.Helper()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(body, &parsed))
	system, ok := parsed["system"].([]any)
	require.True(t, ok, "system should be an array")
	require.Greater(t, len(system), index)
	block, ok := system[index].(map[string]any)
	require.True(t, ok)
	text, ok := block["text"].(string)
	require.True(t, ok)
	return text
}
