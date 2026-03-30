package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// billingBlockCCVersionRegex matches cc_version=X.Y.Z (captures the semver part only,
// leaving any trailing .suffix like .88c untouched).
var billingBlockCCVersionRegex = regexp.MustCompile(`cc_version=(\d+\.\d+\.\d+)`)

// ExtractBillingBlockVersion extracts the semver (e.g. "2.1.86") from the billing
// block in system[0].text. Returns "" if no billing block or no parseable cc_version.
func ExtractBillingBlockVersion(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	firstSystemText := gjson.GetBytes(body, "system.0.text")
	if !firstSystemText.Exists() || firstSystemText.Type != gjson.String {
		return ""
	}
	text := firstSystemText.String()
	if !strings.HasPrefix(text, "x-anthropic-billing-header:") {
		return ""
	}
	matches := billingBlockCCVersionRegex.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// ReconstructUAVersion replaces the semver in a Claude CLI User-Agent string with
// a new version. Example:
//
//	ReconstructUAVersion("claude-cli/2.1.84 (external, cli)", "2.1.86")
//	→ "claude-cli/2.1.86 (external, cli)"
//
// Returns the original UA unchanged if no version pattern is found.
func ReconstructUAVersion(ua, newVersion string) string {
	if ua == "" || newVersion == "" {
		return ua
	}
	// Only replace the first version pattern (claude-cli/X.Y.Z),
	// not subsequent ones like Node.js/22.13.1
	loc := userAgentVersionRegex.FindStringIndex(ua)
	if loc == nil {
		return ua
	}
	return ua[:loc[0]] + "/" + newVersion + ua[loc[1]:]
}

// NormalizeBillingBlock rewrites the cc_version in the first system billing block
// so that its semver portion matches the fingerprinted User-Agent version.
//
// NOTE: This is NOT used in the main gateway forwarding path. The gateway instead
// treats the billing block as the version source of truth and adapts headers to match.
// This function is retained for edge cases where body-level normalization is needed.
func NormalizeBillingBlock(body []byte, fingerprintUA string) []byte {
	if len(body) == 0 || fingerprintUA == "" {
		return body
	}

	targetVersion := ExtractCLIVersion(fingerprintUA)
	if targetVersion == "" {
		return body
	}

	firstSystemText := gjson.GetBytes(body, "system.0.text")
	if !firstSystemText.Exists() || firstSystemText.Type != gjson.String {
		return body
	}

	text := firstSystemText.String()
	if !strings.HasPrefix(text, "x-anthropic-billing-header:") {
		return body
	}

	matches := billingBlockCCVersionRegex.FindStringSubmatch(text)
	if len(matches) < 2 {
		return body
	}

	currentVersion := matches[1]
	if currentVersion == targetVersion {
		return body
	}

	newText := strings.Replace(text, "cc_version="+currentVersion, "cc_version="+targetVersion, 1)

	newBody, err := sjson.SetBytes(body, "system.0.text", newText)
	if err != nil {
		return body
	}
	return newBody
}

// GenerateBillingBlockText creates a billing block string matching real Claude Code
// format, using the given CLI version. The .suffix is a random 3-char hex value.
//
// Output example:
//
//	x-anthropic-billing-header: cc_version=2.1.86.a3f; cc_entrypoint=cli; cch=00000;
func GenerateBillingBlockText(cliVersion string) string {
	suffix := randomHexSuffix(3)
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli; cch=00000;", cliVersion, suffix)
}

// randomHexSuffix returns n random hex characters (n bytes → 2n hex chars, truncated to n).
func randomHexSuffix(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000"
	}
	return hex.EncodeToString(b)[:n]
}
