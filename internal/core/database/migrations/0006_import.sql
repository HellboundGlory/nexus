CREATE TABLE download_queue (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    download_client_id TEXT    NOT NULL,
    client_item_id     TEXT    NOT NULL,
    protocol           TEXT    NOT NULL,
    source_title       TEXT    NOT NULL,
    media_kind         TEXT    NOT NULL,
    series_id          INTEGER REFERENCES series(id) ON DELETE CASCADE,
    movie_id           INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    episode_ids        TEXT    NOT NULL DEFAULT '[]',
    quality_id         INTEGER NOT NULL,
    status             TEXT    NOT NULL,
    error              TEXT    NOT NULL DEFAULT '',
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(download_client_id, client_item_id)
);

CREATE TABLE media_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    media_kind    TEXT    NOT NULL,
    episode_id    INTEGER REFERENCES episodes(id) ON DELETE CASCADE,
    movie_id      INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    relative_path TEXT    NOT NULL,
    size          INTEGER NOT NULL,
    quality_id    INTEGER NOT NULL,
    added_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(episode_id),
    UNIQUE(movie_id)
);

CREATE TABLE history (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type   TEXT    NOT NULL,
    media_kind   TEXT    NOT NULL,
    series_id    INTEGER REFERENCES series(id)   ON DELETE SET NULL,
    episode_id   INTEGER REFERENCES episodes(id) ON DELETE SET NULL,
    movie_id     INTEGER REFERENCES movies(id)   ON DELETE SET NULL,
    source_title TEXT    NOT NULL DEFAULT '',
    quality_id   INTEGER,
    message      TEXT    NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
