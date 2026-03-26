package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type ClaudeUsageServiceSuite struct {
	suite.Suite
	srv     *httptest.Server
	fetcher *claudeUsageService
}

func (s *ClaudeUsageServiceSuite) TearDownTest() {
	if s.srv != nil {
		s.srv.Close()
		s.srv = nil
	}
}

// usageRequestCapture holds captured request data for assertions in the main goroutine.
type usageRequestCapture struct {
	authorization string
	anthropicBeta string
	userAgent     string
	lang          string
	arch          string
	runtime       string
	runtimeVer    string
	acceptLang    string
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_Success() {
	var captured usageRequestCapture

	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.authorization = r.Header.Get("Authorization")
		captured.anthropicBeta = r.Header.Get("anthropic-beta")
		captured.userAgent = r.Header.Get("User-Agent")
		captured.lang = r.Header.Get("X-Stainless-Lang")
		captured.arch = r.Header.Get("X-Stainless-Arch")
		captured.runtime = r.Header.Get("X-Stainless-Runtime")
		captured.runtimeVer = r.Header.Get("X-Stainless-Runtime-Version")
		captured.acceptLang = r.Header.Get("accept-language")

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
  "five_hour": {"utilization": 12.5, "resets_at": "2025-01-01T00:00:00Z"},
  "seven_day": {"utilization": 34.0, "resets_at": "2025-01-08T00:00:00Z"},
  "seven_day_sonnet": {"utilization": 56.0, "resets_at": "2025-01-08T00:00:00Z"}
}`)
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	resp, err := s.fetcher.FetchUsage(context.Background(), "at", "")
	require.NoError(s.T(), err, "FetchUsage")
	require.Equal(s.T(), 12.5, resp.FiveHour.Utilization, "FiveHour utilization mismatch")
	require.Equal(s.T(), 34.0, resp.SevenDay.Utilization, "SevenDay utilization mismatch")
	require.Equal(s.T(), 56.0, resp.SevenDaySonnet.Utilization, "SevenDaySonnet utilization mismatch")

	// Assertions on captured request data
	require.Equal(s.T(), "Bearer at", captured.authorization, "Authorization header mismatch")
	require.Equal(s.T(), "oauth-2025-04-20", captured.anthropicBeta, "anthropic-beta header mismatch")
	require.Equal(s.T(), defaultUsageUserAgent, captured.userAgent, "User-Agent header mismatch")
	require.Equal(s.T(), "js", captured.lang, "X-Stainless-Lang header mismatch")
	require.Equal(s.T(), "x64", captured.arch, "X-Stainless-Arch header mismatch")
	require.Equal(s.T(), "node", captured.runtime, "X-Stainless-Runtime header mismatch")
	require.Equal(s.T(), "v24.13.1", captured.runtimeVer, "X-Stainless-Runtime-Version header mismatch")
	require.Equal(s.T(), "*", captured.acceptLang, "accept-language header mismatch")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsageWithOptions_UsesFingerprintHeaders() {
	var captured usageRequestCapture

	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.userAgent = r.Header.Get("User-Agent")
		captured.lang = r.Header.Get("X-Stainless-Lang")
		captured.arch = r.Header.Get("X-Stainless-Arch")
		captured.runtime = r.Header.Get("X-Stainless-Runtime")
		captured.runtimeVer = r.Header.Get("X-Stainless-Runtime-Version")
		captured.acceptLang = r.Header.Get("accept-language")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"five_hour":{"utilization":0,"resets_at":"2025-01-01T00:00:00Z"},"seven_day":{"utilization":0,"resets_at":"2025-01-08T00:00:00Z"},"seven_day_sonnet":{"utilization":0,"resets_at":"2025-01-08T00:00:00Z"}}`)
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	_, err := s.fetcher.FetchUsageWithOptions(context.Background(), &service.ClaudeUsageFetchOptions{
		AccessToken: "at",
		Fingerprint: &service.Fingerprint{
			UserAgent:               "claude-cli/2.1.84 (external, cli)",
			StainlessLang:           "js",
			StainlessArch:           "x64",
			StainlessRuntime:        "node",
			StainlessRuntimeVersion: "v24.13.1",
		},
	})
	require.NoError(s.T(), err)
	require.Equal(s.T(), "claude-cli/2.1.84 (external, cli)", captured.userAgent)
	require.Equal(s.T(), "js", captured.lang)
	require.Equal(s.T(), "x64", captured.arch)
	require.Equal(s.T(), "node", captured.runtime)
	require.Equal(s.T(), "v24.13.1", captured.runtimeVer)
	require.Equal(s.T(), "*", captured.acceptLang)
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_NonOK() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "nope")
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	_, err := s.fetcher.FetchUsage(context.Background(), "at", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "status 401")
	require.ErrorContains(s.T(), err, "nope")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_BadJSON() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "not-json")
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	_, err := s.fetcher.FetchUsage(context.Background(), "at", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "decode response failed")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_ContextCancel() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond - simulate slow server
		<-r.Context().Done()
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := s.fetcher.FetchUsage(ctx, "at", "")
	require.Error(s.T(), err, "expected error for cancelled context")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_InvalidProxyReturnsError() {
	s.fetcher = &claudeUsageService{
		usageURL:          "http://example.com",
		allowPrivateHosts: true,
	}

	_, err := s.fetcher.FetchUsage(context.Background(), "at", "://bad-proxy-url")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "create http client failed")
}

func TestClaudeUsageServiceSuite(t *testing.T) {
	suite.Run(t, new(ClaudeUsageServiceSuite))
}
