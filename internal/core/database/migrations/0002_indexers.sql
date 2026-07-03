CREATE TABLE indexers (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    implementation TEXT NOT NULL,
    base_url       TEXT NOT NULL,
    api_key        TEXT NOT NULL DEFAULT '',
    enabled        INTEGER NOT NULL DEFAULT 1,
    priority       INTEGER NOT NULL DEFAULT 25,
    categories     TEXT NOT NULL DEFAULT '[]',
    settings       TEXT NOT NULL DEFAULT '{}',
    caps           TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'unknown',
    last_check     DATETIME,
    fail_message   TEXT NOT NULL DEFAULT '',
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
