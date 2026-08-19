package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func assertRandomCodexFingerprintOutbound(
	t *testing.T,
	account *Account,
	upstream *httpUpstreamRecorder,
	legacySessionID string,
) {
	t.Helper()
	require.NotNil(t, upstream.lastReq)

	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	wantInstallation := resolveRandomCodexFingerprintInstallationID(seed)
	wantSession := resolveRandomCodexFingerprintSessionID(seed)
	wantThread := resolveRandomCodexFingerprintThreadID(seed)
	wantWindow := resolveRandomCodexFingerprintWindowID(seed)

	bodyInstallation := gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").String()
	bodySession := gjson.GetBytes(upstream.lastBody, "client_metadata.session_id").String()
	bodyThread := gjson.GetBytes(upstream.lastBody, "client_metadata.thread_id").String()
	bodyTurn := gjson.GetBytes(upstream.lastBody, "client_metadata.turn_id").String()
	bodyWindow := gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-window-id").String()

	require.Equal(t, wantInstallation, bodyInstallation)
	require.Equal(t, wantSession, bodySession)
	require.Equal(t, wantThread, bodyThread)
	require.NotEmpty(t, bodyTurn)
	require.Equal(t, wantWindow, bodyWindow)

	header := upstream.lastReq.Header
	require.Equal(t, bodyInstallation, header.Get("x-codex-installation-id"))
	require.Equal(t, bodySession, header.Get("session-id"))
	require.Equal(t, bodySession, header.Get("session_id"))
	require.Equal(t, bodyThread, header.Get("thread-id"))
	require.Equal(t, bodyThread, header.Get("x-client-request-id"))
	require.Equal(t, bodyWindow, header.Get("x-codex-window-id"))
	if legacySessionID != "" {
		require.NotEqual(t, legacySessionID, header.Get("session_id"))
	}

	turnMetadata := gjson.Parse(header.Get("x-codex-turn-metadata"))
	require.Equal(t, bodyInstallation, turnMetadata.Get("installation_id").String())
	require.Equal(t, bodySession, turnMetadata.Get("session_id").String())
	require.Equal(t, bodyThread, turnMetadata.Get("thread_id").String())
	require.Equal(t, bodyTurn, turnMetadata.Get("turn_id").String())
	require.Equal(t, bodyWindow, turnMetadata.Get("window_id").String())
	require.Equal(t, "seatbelt", turnMetadata.Get("sandbox").String())
}

func assertCodexFingerprintFlatOutbound(
	t *testing.T,
	account *Account,
	req *http.Request,
	body []byte,
	clientSessionID string,
) {
	t.Helper()
	require.NotNil(t, account)
	require.NotNil(t, req)

	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	mode := account.GetCodexFingerprintMode()

	wantInstallation := resolveCodexFingerprintInstallationID(account, mode, seed)
	var wantSession, wantThread, wantWindow string
	switch mode {
	case codexFingerprintSession:
		wantSession = resolveConvergedSessionID(seed)
		wantThread = resolveConvergedThreadID(seed, clientSessionID)
		if wantThread == "" {
			wantThread = wantSession
		}
		wantWindow = wantThread + ":0"
	case codexFingerprintFull:
		wantSession = resolveConvergedSessionID(seed)
		wantThread = wantSession
		wantWindow = wantThread + ":0"
	case codexFingerprintRandom:
		wantSession = resolveRandomCodexFingerprintSessionID(seed)
		wantThread = resolveRandomCodexFingerprintThreadID(seed)
		wantWindow = resolveRandomCodexFingerprintWindowID(seed)
	case codexFingerprintRandomMulti:
		wantSession = resolveRandomCodexFingerprintSessionID(seed)
		wantThread = resolveRandomMultiCodexFingerprintThreadID(seed, clientSessionID)
		wantWindow = resolveRandomMultiCodexFingerprintWindowID(seed, clientSessionID)
	default:
		t.Fatalf("unsupported fingerprint mode for flat parity assertion: %q", mode)
	}

	bodyInstallation := gjson.GetBytes(body, "client_metadata.x-codex-installation-id").String()
	bodySession := gjson.GetBytes(body, "client_metadata.session_id").String()
	bodyThread := gjson.GetBytes(body, "client_metadata.thread_id").String()
	bodyTurn := gjson.GetBytes(body, "client_metadata.turn_id").String()
	bodyWindow := gjson.GetBytes(body, "client_metadata.x-codex-window-id").String()

	require.Equal(t, wantInstallation, bodyInstallation)
	require.Equal(t, wantSession, bodySession)
	require.Equal(t, wantThread, bodyThread)
	require.NotEmpty(t, bodyTurn)
	require.Equal(t, wantWindow, bodyWindow)

	header := req.Header
	require.Equal(t, bodyInstallation, header.Get("x-codex-installation-id"))
	require.Equal(t, bodySession, header.Get("session-id"))
	require.Equal(t, bodySession, header.Get("session_id"))
	require.Equal(t, bodyThread, header.Get("thread-id"))
	require.Equal(t, bodyThread, header.Get("x-client-request-id"))
	require.Equal(t, bodyWindow, header.Get("x-codex-window-id"))

	if raw := strings.TrimSpace(header.Get("x-codex-turn-metadata")); raw != "" {
		turnMetadata := gjson.Parse(raw)
		require.Equal(t, bodyInstallation, turnMetadata.Get("installation_id").String())
		require.Equal(t, bodySession, turnMetadata.Get("session_id").String())
		require.Equal(t, bodyThread, turnMetadata.Get("thread_id").String())
		require.Equal(t, bodyTurn, turnMetadata.Get("turn_id").String())
		require.Equal(t, bodyWindow, turnMetadata.Get("window_id").String())
	}
}

func newRandomCodexFingerprintCompatAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Name:        "openai-oauth-random",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{
			codexFingerprintModeExtraKey: "random",
			codexFingerprintSeedExtraKey: testCodexFingerprintSeed,
		},
	}
}

func TestForwardAsChatCompletions_RandomFingerprintKeepsBodyAndFinalHeadersAligned(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("session-id", "client-session")
	c.Request.Header.Set("session_id", "client-session")
	c.Request.Header.Set("x-codex-turn-metadata", `{"installation_id":"client-install","session_id":"client-session","thread_id":"client-thread","turn_id":"client-turn","window_id":"client-window","sandbox":"seatbelt"}`)
	c.Set("api_key", &APIKey{ID: 91})

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid_chat_random_fingerprint"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop after recording"}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	account := newRandomCodexFingerprintCompatAccount(9101)

	_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "legacy-cache-key", "gpt-5.4")
	require.Error(t, err)
	legacySessionID := generateSessionUUID(isolateOpenAISessionID(91, "legacy-cache-key"))
	assertRandomCodexFingerprintOutbound(t, account, upstream, legacySessionID)
}

func TestForwardAsAnthropic_RandomFingerprintKeepsBodyAndFinalHeadersAligned(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("session-id", "client-session")
	c.Request.Header.Set("session_id", "client-session")
	c.Request.Header.Set("x-codex-turn-metadata", `{"installation_id":"client-install","session_id":"client-session","thread_id":"client-thread","turn_id":"client-turn","window_id":"client-window","sandbox":"seatbelt"}`)
	c.Set("api_key", &APIKey{ID: 92})

	upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_random_fingerprint", "gpt-5.4")}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false},
		}},
		httpUpstream: upstream,
	}
	account := newRandomCodexFingerprintCompatAccount(9201)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "legacy-cache-key", "gpt-5.4")
	require.NoError(t, err)
	require.NotNil(t, result)
	legacySessionID := generateSessionUUID(isolateOpenAISessionID(92, "legacy-cache-key"))
	assertRandomCodexFingerprintOutbound(t, account, upstream, legacySessionID)
}
