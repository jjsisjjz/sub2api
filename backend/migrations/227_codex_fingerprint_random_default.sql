-- Normalize OpenAI OAuth Codex fingerprints to independent random mode.
-- The migration is idempotent: canonical, non-duplicate seeds are preserved.

-- Canonicalize supported modes and send missing, blank, or unsupported values to
-- the new default. Case and surrounding whitespace do not turn an explicit off
-- into random.
UPDATE accounts
SET extra = jsonb_set(
    COALESCE(extra, '{}'::jsonb),
    '{codex_fingerprint_mode}',
    to_jsonb(
        CASE
            WHEN lower(btrim(COALESCE(extra->>'codex_fingerprint_mode', ''))) IN ('off', 'device', 'session', 'full', 'random', 'random_multi')
                THEN lower(btrim(extra->>'codex_fingerprint_mode'))
            ELSE 'random'
        END
    ),
    true
)
WHERE deleted_at IS NULL
  AND platform = 'openai'
  AND type = 'oauth'
  AND COALESCE(extra->>'codex_fingerprint_mode', '') IS DISTINCT FROM
      CASE
          WHEN lower(btrim(COALESCE(extra->>'codex_fingerprint_mode', ''))) IN ('off', 'device', 'session', 'full', 'random', 'random_multi')
              THEN lower(btrim(extra->>'codex_fingerprint_mode'))
          ELSE 'random'
      END;

-- Repair missing, malformed, and nil seeds for every active fingerprint mode.
UPDATE accounts
SET extra = jsonb_set(
    COALESCE(extra, '{}'::jsonb),
    '{codex_fingerprint_seed}',
    to_jsonb(gen_random_uuid()::text),
    true
)
WHERE deleted_at IS NULL
  AND platform = 'openai'
  AND type = 'oauth'
  AND COALESCE(extra->>'codex_fingerprint_mode', 'random') IN ('device', 'session', 'full', 'random', 'random_multi')
  AND NOT (
      extra->>'codex_fingerprint_seed' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
      AND extra->>'codex_fingerprint_seed' <> '00000000-0000-0000-0000-000000000000'
  );

-- Keep the lowest account id for an existing seed and mint a fresh seed for every
-- duplicate row, including explicit-off rows. Off rows retain a unique seed so a
-- later re-enable cannot collide with the partial lifecycle state of another row.
WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY extra->>'codex_fingerprint_seed'
               ORDER BY id
           ) AS duplicate_rank
    FROM accounts
    WHERE deleted_at IS NULL
      AND platform = 'openai'
      AND type = 'oauth'
      AND extra->>'codex_fingerprint_seed' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
      AND extra->>'codex_fingerprint_seed' <> '00000000-0000-0000-0000-000000000000'
)
UPDATE accounts AS account
SET extra = jsonb_set(
    COALESCE(account.extra, '{}'::jsonb),
    '{codex_fingerprint_seed}',
    to_jsonb(gen_random_uuid()::text),
    true
)
FROM ranked
WHERE account.id = ranked.id
  AND ranked.duplicate_rank > 1;

-- Guard future writes. Explicit-off rows participate because they may later be
-- re-enabled while preserving their account-lifecycle seed.
CREATE UNIQUE INDEX IF NOT EXISTS accounts_openai_oauth_codex_fingerprint_seed_uidx
ON accounts ((extra->>'codex_fingerprint_seed'))
WHERE deleted_at IS NULL
  AND platform = 'openai'
  AND type = 'oauth'
  AND extra->>'codex_fingerprint_seed' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  AND extra->>'codex_fingerprint_seed' <> '00000000-0000-0000-0000-000000000000';
