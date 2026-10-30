-- Shared by runtime schema creation and sqlc. This is the current schema,
-- not a migration or a second independently maintained schema snapshot.
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
