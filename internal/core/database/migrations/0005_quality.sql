CREATE TABLE quality_profiles (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    name              TEXT NOT NULL UNIQUE,
    cutoff_quality_id INTEGER NOT NULL,
    upgrade_allowed   INTEGER NOT NULL DEFAULT 1,
    items             TEXT NOT NULL,
    created_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
