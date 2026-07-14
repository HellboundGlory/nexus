CREATE TABLE blocklist (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    media_kind   TEXT    NOT NULL,
    movie_id     INTEGER REFERENCES movies(id) ON DELETE CASCADE,
    series_id    INTEGER REFERENCES series(id) ON DELETE CASCADE,
    source_title TEXT    NOT NULL,
    norm_title   TEXT    NOT NULL,
    protocol     TEXT    NOT NULL DEFAULT '',
    quality_id   INTEGER NOT NULL DEFAULT 0,
    reason       TEXT    NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_blocklist_movie  ON blocklist(movie_id);
CREATE INDEX idx_blocklist_series ON blocklist(series_id);

-- Queue is now transient: clear pre-existing terminal rows so the Queue view
-- shows only active downloads immediately after upgrade.
DELETE FROM download_queue WHERE status IN ('imported','failed');
