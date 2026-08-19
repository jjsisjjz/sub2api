package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration227CodexFingerprintRandomDefaultContract(t *testing.T) {
	content, err := FS.ReadFile("227_codex_fingerprint_random_default.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "lower(btrim(COALESCE(extra->>'codex_fingerprint_mode', '')))")
	require.Contains(t, sql, "IN ('off', 'device', 'session', 'full', 'random', 'random_multi')")
	require.Contains(t, sql, "ELSE 'random'")
	require.Contains(t, sql, "COALESCE(extra->>'codex_fingerprint_mode', 'random') IN ('device', 'session', 'full', 'random', 'random_multi')")
	require.Contains(t, sql, "row_number() OVER")
	require.Contains(t, sql, "PARTITION BY extra->>'codex_fingerprint_seed'")
	require.Contains(t, sql, "ranked.duplicate_rank > 1")
	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS accounts_openai_oauth_codex_fingerprint_seed_uidx")
	require.NotContains(t, sql, "COALESCE(extra->>'codex_fingerprint_mode', 'random') <> 'off'")
	require.NotContains(t, sql, "extra = COALESCE(extra, '{}'::jsonb) - 'codex_fingerprint_seed'")
}
