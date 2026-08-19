package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexQuotaOverdraftProbeRandomMultiFingerprintParity(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(`data: {"type":"response.completed","response":{"output":[]}}

`)),
	}}
	coordinator := &CodexQuotaOverdraftCoordinator{httpUpstream: upstream}
	account := newRandomCodexFingerprintCompatAccount(4321)
	account.Extra[codexFingerprintModeExtraKey] = string(codexFingerprintRandomMulti)

	result := coordinator.runProbeAttempt(context.Background(), account, "gpt-5.4")
	require.Equal(t, "available", result.Status)
	require.Equal(t, "model_response_ok", result.ReasonCode)
	assertCodexFingerprintFlatOutbound(t, account, upstream.lastReq, upstream.lastBody, "")
}
