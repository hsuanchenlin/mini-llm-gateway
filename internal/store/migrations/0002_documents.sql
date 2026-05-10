CREATE TABLE IF NOT EXISTS documents (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    body         TEXT NOT NULL,
    chunk_count  INTEGER NOT NULL,
    created_ms   INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS documents_created_ms_idx ON documents(created_ms DESC);
