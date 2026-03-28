package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"

	utls "github.com/refraction-networking/utls"

	"github.com/imroc/req/v3"
)

func NewClaudeOAuthClient() service.ClaudeOAuthClient {
	return &claudeOAuthService{
		baseURL:              "https://claude.ai",
		tokenURL:             oauth.TokenURL,
		browserClientFactory: createClaudeOAuthBrowserClient,
		tokenClientFactory:   createClaudeOAuthTokenClient,
	}
}

type claudeOAuthService struct {
	baseURL              string
	tokenURL             string
	browserClientFactory func(proxyURL string) (*req.Client, error)
	tokenClientFactory   func(proxyURL string) (*req.Client, error)
}

func (s *claudeOAuthService) GetOrganizationUUID(ctx context.Context, sessionKey, proxyURL string) (string, error) {
	client, err := s.browserClientFactory(proxyURL)
	if err != nil {
		return "", fmt.Errorf("create HTTP client: %w", err)
	}

	var orgs []struct {
		UUID      string  `json:"uuid"`
		Name      string  `json:"name"`
		RavenType *string `json:"raven_type"`
	}

	targetURL := s.baseURL + "/api/organizations"
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1: Getting organization UUID from %s", targetURL)

	resp, err := client.R().
		SetContext(ctx).
		SetCookies(&http.Cookie{Name: "sessionKey", Value: sessionKey}).
		SetSuccessResult(&orgs).
		Get(targetURL)
	if err != nil {
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 FAILED - Request error: %v", err)
		return "", fmt.Errorf("request failed: %w", err)
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 Response - Status: %d", resp.StatusCode)
	if !resp.IsSuccessState() {
		return "", fmt.Errorf("failed to get organizations: status %d, body: %s", resp.StatusCode, resp.String())
	}
	if len(orgs) == 0 {
		return "", fmt.Errorf("no organizations found")
	}
	if len(orgs) == 1 {
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 SUCCESS - Single org found, UUID: %s, Name: %s", orgs[0].UUID, orgs[0].Name)
		return orgs[0].UUID, nil
	}
	for _, org := range orgs {
		if org.RavenType != nil && *org.RavenType == "team" {
			logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 SUCCESS - Selected team org, UUID: %s, Name: %s, RavenType: %s", org.UUID, org.Name, *org.RavenType)
			return org.UUID, nil
		}
	}
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 1 SUCCESS - No team org found, using first org, UUID: %s, Name: %s", orgs[0].UUID, orgs[0].Name)
	return orgs[0].UUID, nil
}

func (s *claudeOAuthService) GetAuthorizationCode(ctx context.Context, sessionKey, orgUUID, scope, codeChallenge, state, proxyURL string) (string, error) {
	client, err := s.browserClientFactory(proxyURL)
	if err != nil {
		return "", fmt.Errorf("create HTTP client: %w", err)
	}

	authURL := fmt.Sprintf("%s/v1/oauth/%s/authorize", s.baseURL, orgUUID)
	reqBody := map[string]any{
		"response_type":         "code",
		"client_id":             oauth.ClientID,
		"organization_uuid":     orgUUID,
		"redirect_uri":          oauth.RedirectURI,
		"scope":                 scope,
		"state":                 state,
		"code_challenge":        codeChallenge,
		"code_challenge_method": "S256",
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2: Getting authorization code from %s", authURL)
	reqBodyJSON, _ := json.Marshal(logredact.RedactMap(reqBody))
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 Request Body: %s", string(reqBodyJSON))

	var result struct {
		RedirectURI string `json:"redirect_uri"`
	}

	resp, err := client.R().
		SetContext(ctx).
		SetCookies(&http.Cookie{Name: "sessionKey", Value: sessionKey}).
		SetHeader("Accept", oauth.BrowserAuthorizeAccept).
		SetHeader("Accept-Language", oauth.BrowserAuthorizeAcceptLanguage).
		SetHeader("Cache-Control", oauth.BrowserAuthorizeCacheControl).
		SetHeader("Origin", oauth.BrowserAuthorizeOrigin).
		SetHeader("Referer", oauth.BrowserAuthorizeReferer).
		SetHeader("Content-Type", "application/json").
		SetBody(reqBody).
		SetSuccessResult(&result).
		Post(authURL)
	if err != nil {
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 FAILED - Request error: %v", err)
		return "", fmt.Errorf("request failed: %w", err)
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 Response - Status: %d, Body: %s", resp.StatusCode, logredact.RedactJSON(resp.Bytes()))
	if !resp.IsSuccessState() {
		return "", fmt.Errorf("failed to get authorization code: status %d, body: %s", resp.StatusCode, resp.String())
	}
	if result.RedirectURI == "" {
		return "", fmt.Errorf("no redirect_uri in response")
	}

	parsedURL, err := url.Parse(result.RedirectURI)
	if err != nil {
		return "", fmt.Errorf("failed to parse redirect_uri: %w", err)
	}
	queryParams := parsedURL.Query()
	authCode := queryParams.Get("code")
	responseState := queryParams.Get("state")
	if authCode == "" {
		return "", fmt.Errorf("no authorization code in redirect_uri")
	}
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 2 SUCCESS - Got authorization code")
	if responseState != "" {
		return authCode + "#" + responseState, nil
	}
	return authCode, nil
}

func (s *claudeOAuthService) ExchangeCodeForToken(ctx context.Context, code, codeVerifier, state, proxyURL string, isSetupToken bool) (*oauth.TokenResponse, error) {
	client, err := s.tokenClientFactory(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}

	authCode := code
	codeState := ""
	if idx := strings.Index(code, "#"); idx != -1 {
		authCode = code[:idx]
		codeState = code[idx+1:]
	}

	reqBody := map[string]any{
		"code":          authCode,
		"grant_type":    "authorization_code",
		"client_id":     oauth.ClientID,
		"redirect_uri":  oauth.RedirectURI,
		"code_verifier": codeVerifier,
	}
	if codeState != "" {
		reqBody["state"] = codeState
	}
	if isSetupToken {
		reqBody["expires_in"] = 31536000
	}

	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3: Exchanging code for token at %s", s.tokenURL)
	reqBodyJSON, _ := json.Marshal(logredact.RedactMap(reqBody))
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 Request Body: %s", string(reqBodyJSON))

	var tokenResp oauth.TokenResponse
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Accept", oauth.TokenAccept).
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", oauth.TokenUserAgent).
		SetBody(reqBody).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)
	if err != nil {
		logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 FAILED - Request error: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 Response - Status: %d, Body: %s", resp.StatusCode, logredact.RedactJSON(resp.Bytes()))
	if !resp.IsSuccessState() {
		return nil, fmt.Errorf("token exchange failed: status %d, body: %s", resp.StatusCode, resp.String())
	}
	logger.LegacyPrintf("repository.claude_oauth", "[OAuth] Step 3 SUCCESS - Got access token")
	return &tokenResp, nil
}

func (s *claudeOAuthService) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*oauth.TokenResponse, error) {
	client, err := s.tokenClientFactory(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("create HTTP client: %w", err)
	}

	reqBody := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     oauth.ClientID,
	}

	var tokenResp oauth.TokenResponse
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("Accept", oauth.TokenAccept).
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", oauth.TokenUserAgent).
		SetBody(reqBody).
		SetSuccessResult(&tokenResp).
		Post(s.tokenURL)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if !resp.IsSuccessState() {
		return nil, fmt.Errorf("token refresh failed: status %d, body: %s", resp.StatusCode, resp.String())
	}
	return &tokenResp, nil
}

func createClaudeOAuthBrowserClient(proxyURL string) (*req.Client, error) {
	client := req.C().SetTimeout(60 * time.Second).ImpersonateChrome().SetCookieJar(nil)
	trimmed, _, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if trimmed != "" {
		client.SetProxyURL(trimmed)
	}
	return client, nil
}

func createClaudeOAuthTokenClient(proxyURL string) (*req.Client, error) {
	_, parsedProxy, err := proxyurl.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	transport, err := buildUpstreamTransportWithTLSFingerprint(poolSettings{
		maxIdleConns:          4,
		maxIdleConnsPerHost:   4,
		maxConnsPerHost:       4,
		idleConnTimeout:       90 * time.Second,
		responseHeaderTimeout: 60 * time.Second,
	}, parsedProxy, claudeOAuthTokenTLSProfile())
	if err != nil {
		return nil, err
	}

	client := req.C().SetTimeout(60 * time.Second).SetCookieJar(nil)
	client.GetClient().Transport = transport
	return client, nil
}

func claudeOAuthTokenTLSProfile() *tlsfingerprint.Profile {
	return &tlsfingerprint.Profile{
		Name:                "claude_oauth_token",
		CipherSuites:        []uint16{0x1301, 0x1302, 0x1303, 0xc02b, 0xc02f, 0xc02c, 0xc030, 0xcca9, 0xcca8, 0xc009, 0xc013, 0xc00a, 0xc014, 0x009c, 0x009d, 0x002f, 0x0035},
		Curves:              []uint16{uint16(utls.X25519), uint16(utls.CurveP256), uint16(utls.CurveP384)},
		PointFormats:        []uint16{0},
		SignatureAlgorithms: []uint16{0x0403, 0x0804, 0x0401, 0x0503, 0x0805, 0x0501, 0x0806, 0x0601, 0x0201},
		ALPNProtocols:       []string{"http/1.1"},
		SupportedVersions:   []uint16{utls.VersionTLS13, utls.VersionTLS12},
		KeyShareGroups:      []uint16{uint16(utls.X25519)},
		PSKModes:            []uint16{uint16(utls.PskModeDHE)},
		Extensions:          []uint16{0, 65037, 23, 65281, 10, 11, 35, 16, 5, 13, 18, 51, 45, 43},
	}
}
