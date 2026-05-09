CREATE TABLE IF NOT EXISTS requests (
    id                TEXT PRIMARY KEY,
    ts_ms             INTEGER NOT NULL,
    provider          TEXT NOT NULL,
    model             TEXT NOT NULL,
    latency_ms        INTEGER NOT NULL,
    status_code       INTEGER NOT NULL,
    error_text        TEXT NOT NULL DEFAULT '',
    prompt_chars      INTEGER NOT NULL DEFAULT 0,
    completion_chars  INTEGER NOT NULL DEFAULT 0,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS requests_ts_ms_idx ON requests(ts_ms DESC);
