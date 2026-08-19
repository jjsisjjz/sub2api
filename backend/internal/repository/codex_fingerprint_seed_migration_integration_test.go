//go:build integration

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func requireCanonicalUUIDString(t *testing.T, value string) {
	t.Helper()
	parsed, err := uuid.Parse(value)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, parsed)
	require.Equal(t, parsed.String(), value)
}

func TestMigration225BackfillsOnlyEnabledOpenAIOAuthMissingOrMalformedSeeds(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("225_backfill_codex_fingerprint_seed.sql")
	require.NoError(t, err)

	var missingID, blankID, malformedID, validID, offID, apiKeyID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-missing', 'openai', 'oauth', '{"codex_fingerprint_mode":"session"}'::jsonb)
RETURNING id
`).Scan(&missingID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-blank', 'openai', 'oauth', '{"codex_fingerprint_mode":"device","codex_fingerprint_seed":""}'::jsonb)
RETURNING id
`).Scan(&blankID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-malformed', 'openai', 'oauth', '{"codex_fingerprint_mode":"full","codex_fingerprint_seed":"BAD"}'::jsonb)
RETURNING id
`).Scan(&malformedID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-valid', 'openai', 'oauth', '{"codex_fingerprint_mode":"session","codex_fingerprint_seed":"11111111-1111-4111-8111-111111111111"}'::jsonb)
RETURNING id
`).Scan(&validID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-off', 'openai', 'oauth', '{"codex_fingerprint_mode":"off"}'::jsonb)
RETURNING id
`).Scan(&offID))
	require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-225-apikey', 'openai', 'apikey', '{"codex_fingerprint_mode":"session"}'::jsonb)
RETURNING id
`).Scan(&apiKeyID))

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	seedsAfterFirst := map[int64]string{}
	for _, id := range []int64{missingID, blankID, malformedID, validID} {
		var seed string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra->>'codex_fingerprint_seed' FROM accounts WHERE id = $1`, id).Scan(&seed))
		requireCanonicalUUIDString(t, seed)
		seedsAfterFirst[id] = seed
	}
	require.Equal(t, "11111111-1111-4111-8111-111111111111", seedsAfterFirst[validID])

	for _, id := range []int64{offID, apiKeyID} {
		var hasSeed bool
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra ? 'codex_fingerprint_seed' FROM accounts WHERE id = $1`, id).Scan(&hasSeed))
		require.False(t, hasSeed)
	}

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	for id, want := range seedsAfterFirst {
		var got string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra->>'codex_fingerprint_seed' FROM accounts WHERE id = $1`, id).Scan(&got))
		require.Equal(t, want, got)
	}
}

func TestMigration227NormalizesRandomFingerprintsAndEnforcesUniqueActiveSeeds(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("227_codex_fingerprint_random_default.sql")
	require.NoError(t, err)
	require.NoError(t, func() error {
		_, dropErr := tx.ExecContext(ctx, `DROP INDEX IF EXISTS accounts_openai_oauth_codex_fingerprint_seed_uidx`)
		return dropErr
	}())

	const (
		validSeed     = "11111111-1111-4111-8111-111111111111"
		secondSeed    = "22222222-2222-4222-8222-222222222222"
		duplicateSeed = "33333333-3333-4333-8333-333333333333"
		offSeed       = "44444444-4444-4444-8444-444444444444"
	)
	type fixture struct {
		name      string
		platform  string
		typeName  string
		extra     string
		deleted   bool
		unchanged bool
	}
	fixtures := []fixture{
		{name: "missing-mode", platform: "openai", typeName: "oauth", extra: `{"marker":"missing-mode"}`},
		{name: "blank-mode", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"","codex_fingerprint_seed":""}`},
		{name: "invalid-mode-valid-seed", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"legacy","codex_fingerprint_seed":"` + validSeed + `"}`},
		{name: "random-valid", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"random","codex_fingerprint_seed":"` + secondSeed + `"}`},
		{name: "random-multi-valid", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"random_multi","codex_fingerprint_seed":"77777777-7777-4777-8777-777777777777"}`},
		{name: "legacy-session-valid", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"session","codex_fingerprint_seed":"55555555-5555-4555-8555-555555555555"}`},
		{name: "nil-seed", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"full","codex_fingerprint_seed":"00000000-0000-0000-0000-000000000000"}`},
		{name: "duplicate-low", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"random","codex_fingerprint_seed":"` + duplicateSeed + `"}`},
		{name: "duplicate-high", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"device","codex_fingerprint_seed":"` + duplicateSeed + `"}`},
		{name: "off", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"off","codex_fingerprint_seed":"` + offSeed + `","marker":"off"}`, unchanged: true},
		{name: "off-whitespace", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":" OFF ","codex_fingerprint_seed":"66666666-6666-4666-8666-666666666666"}`},
		{name: "off-duplicate", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"off","codex_fingerprint_seed":"` + secondSeed + `"}`},
		{name: "api-key", platform: "openai", typeName: "apikey", extra: `{"codex_fingerprint_mode":"broken","codex_fingerprint_seed":"` + validSeed + `","marker":"api-key"}`, unchanged: true},
		{name: "non-openai", platform: "anthropic", typeName: "oauth", extra: `{"codex_fingerprint_mode":"broken","codex_fingerprint_seed":"` + validSeed + `","marker":"non-openai"}`, unchanged: true},
		{name: "deleted", platform: "openai", typeName: "oauth", extra: `{"codex_fingerprint_mode":"broken","codex_fingerprint_seed":"` + validSeed + `","marker":"deleted"}`, deleted: true, unchanged: true},
	}

	ids := make(map[string]int64, len(fixtures))
	before := make(map[string]string, len(fixtures))
	for _, item := range fixtures {
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra, deleted_at)
VALUES ($1, $2, $3, $4::jsonb, CASE WHEN $5 THEN NOW() ELSE NULL END)
RETURNING id
`, "migration-227-"+item.name, item.platform, item.typeName, item.extra, item.deleted).Scan(&id))
		ids[item.name] = id
		var stored string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra::text FROM accounts WHERE id = $1`, id).Scan(&stored))
		before[item.name] = stored
	}

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	readModeSeed := func(name string) (string, string) {
		t.Helper()
		var mode, seed string
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT COALESCE(extra->>'codex_fingerprint_mode', ''), COALESCE(extra->>'codex_fingerprint_seed', '')
FROM accounts WHERE id = $1
`, ids[name]).Scan(&mode, &seed))
		return mode, seed
	}
	for _, name := range []string{"missing-mode", "blank-mode", "invalid-mode-valid-seed", "random-valid"} {
		mode, _ := readModeSeed(name)
		require.Equal(t, "random", mode, name)
	}
	mode, seed := readModeSeed("legacy-session-valid")
	require.Equal(t, "session", mode)
	require.Equal(t, "55555555-5555-4555-8555-555555555555", seed)
	mode, seed = readModeSeed("random-multi-valid")
	require.Equal(t, "random_multi", mode)
	require.Equal(t, "77777777-7777-4777-8777-777777777777", seed)
	mode, seed = readModeSeed("off-whitespace")
	require.Equal(t, "off", mode)
	require.Equal(t, "66666666-6666-4666-8666-666666666666", seed)

	for _, name := range []string{"missing-mode", "blank-mode", "nil-seed"} {
		_, repaired := readModeSeed(name)
		requireCanonicalUUIDString(t, repaired)
		require.NotEqual(t, "00000000-0000-0000-0000-000000000000", repaired)
	}
	_, seed = readModeSeed("invalid-mode-valid-seed")
	require.Equal(t, validSeed, seed)
	_, seed = readModeSeed("random-valid")
	require.Equal(t, secondSeed, seed)
	_, deduplicatedOffSeed := readModeSeed("off-duplicate")
	requireCanonicalUUIDString(t, deduplicatedOffSeed)
	require.NotEqual(t, secondSeed, deduplicatedOffSeed)
	_, lowDuplicate := readModeSeed("duplicate-low")
	_, highDuplicate := readModeSeed("duplicate-high")
	require.Equal(t, duplicateSeed, lowDuplicate)
	requireCanonicalUUIDString(t, highDuplicate)
	require.NotEqual(t, duplicateSeed, highDuplicate)

	for _, item := range fixtures {
		if !item.unchanged {
			continue
		}
		var after string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra::text FROM accounts WHERE id = $1`, ids[item.name]).Scan(&after))
		require.Equal(t, before[item.name], after, item.name)
	}

	afterFirst := make(map[string]string, len(fixtures))
	for _, item := range fixtures {
		var value string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra::text FROM accounts WHERE id = $1`, ids[item.name]).Scan(&value))
		afterFirst[item.name] = value
	}
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	for _, item := range fixtures {
		var afterSecond string
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra::text FROM accounts WHERE id = $1`, ids[item.name]).Scan(&afterSecond))
		require.Equal(t, afterFirst[item.name], afterSecond, item.name)
	}

	var indexDefinition string
	require.NoError(t, tx.QueryRowContext(ctx, `
SELECT indexdef FROM pg_indexes
WHERE schemaname = current_schema()
  AND indexname = 'accounts_openai_oauth_codex_fingerprint_seed_uidx'
`).Scan(&indexDefinition))
	require.Contains(t, indexDefinition, "CREATE UNIQUE INDEX")
	require.Contains(t, indexDefinition, "deleted_at IS NULL")
	require.Contains(t, indexDefinition, "platform = 'openai'")
	require.Contains(t, indexDefinition, "type = 'oauth'")
	require.NotContains(t, indexDefinition, "<> 'off'")

	require.NoError(t, func() error {
		_, savepointErr := tx.ExecContext(ctx, `SAVEPOINT duplicate_active_seed`)
		return savepointErr
	}())
	_, err = tx.ExecContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-227-index-reject', 'openai', 'oauth', $1::jsonb)
`, `{"codex_fingerprint_mode":"random","codex_fingerprint_seed":"`+secondSeed+`"}`)
	require.Error(t, err)
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
	require.NoError(t, func() error {
		_, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT duplicate_active_seed`)
		return rollbackErr
	}())

	require.NoError(t, func() error {
		_, savepointErr := tx.ExecContext(ctx, `SAVEPOINT duplicate_off_seed`)
		return savepointErr
	}())
	_, err = tx.ExecContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ('migration-227-index-rejects-off-duplicate', 'openai', 'oauth', $1::jsonb)
`, `{"codex_fingerprint_mode":"off","codex_fingerprint_seed":"`+secondSeed+`"}`)
	require.Error(t, err)
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
	require.NoError(t, func() error {
		_, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT duplicate_off_seed`)
		return rollbackErr
	}())
}

func TestBulkUpdateGeneratesDistinctStableCodexFingerprintSeedsPerEligibleRow(t *testing.T) {
	ctx := context.Background()
	testName := "bulk-codex-seed-" + uuid.NewString()
	type fixture struct {
		name        string
		accountType string
		extra       string
	}
	fixtures := []fixture{
		{name: testName + "-missing-a", accountType: service.AccountTypeOAuth, extra: `{}`},
		{name: testName + "-missing-b", accountType: service.AccountTypeOAuth, extra: `{"codex_fingerprint_seed":"BAD"}`},
		{name: testName + "-valid", accountType: service.AccountTypeOAuth, extra: `{"codex_fingerprint_seed":"11111111-1111-4111-8111-111111111111"}`},
		{name: testName + "-apikey", accountType: service.AccountTypeAPIKey, extra: `{}`},
	}

	ids := make([]int64, 0, len(fixtures))
	for _, f := range fixtures {
		var id int64
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ($1, 'openai', $2, $3::jsonb)
RETURNING id
`, f.name, f.accountType, f.extra).Scan(&id))
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM scheduler_outbox WHERE account_id = ANY($1)`, pq.Array(ids))
		_, _ = integrationDB.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = ANY($1)`, pq.Array(ids))
	})

	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	updates := service.AccountBulkUpdate{
		Extra: map[string]any{
			"codex_fingerprint_mode": "session",
		},
		EnsureCodexFingerprintSeed: true,
	}
	rows, err := repo.BulkUpdate(ctx, ids, updates)
	require.NoError(t, err)
	require.Equal(t, int64(len(ids)), rows)

	readSeed := func(id int64) string {
		t.Helper()
		var seed string
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE(extra->>'codex_fingerprint_seed', '') FROM accounts WHERE id = $1`, id).Scan(&seed))
		return seed
	}
	firstSeeds := []string{readSeed(ids[0]), readSeed(ids[1]), readSeed(ids[2]), readSeed(ids[3])}
	requireCanonicalUUIDString(t, firstSeeds[0])
	requireCanonicalUUIDString(t, firstSeeds[1])
	require.NotEqual(t, firstSeeds[0], firstSeeds[1], "gen_random_uuid must be evaluated per eligible row")
	require.Equal(t, "11111111-1111-4111-8111-111111111111", firstSeeds[2])
	require.Empty(t, firstSeeds[3], "API-key accounts must not receive a Codex fingerprint seed")

	rows, err = repo.BulkUpdate(ctx, ids, updates)
	require.NoError(t, err)
	require.Equal(t, int64(len(ids)), rows)
	for i, want := range firstSeeds {
		require.Equal(t, want, readSeed(ids[i]), "retry must not rotate an existing valid seed")
	}
}
