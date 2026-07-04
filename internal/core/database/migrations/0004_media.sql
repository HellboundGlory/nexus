CREATE TABLE root_folders (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE series (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id            INTEGER NOT NULL UNIQUE,
    title              TEXT NOT NULL,
    sort_title         TEXT NOT NULL DEFAULT '',
    overview           TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT '',
    first_aired        TEXT NOT NULL DEFAULT '',
    poster_url         TEXT NOT NULL DEFAULT '',
    fanart_url         TEXT NOT NULL DEFAULT '',
    root_folder_id     INTEGER REFERENCES root_folders(id),
    quality_profile_id INTEGER,
    monitored          INTEGER NOT NULL DEFAULT 1,
    added_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_refreshed_at  DATETIME
);

CREATE TABLE seasons (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id     INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_number INTEGER NOT NULL,
    monitored     INTEGER NOT NULL DEFAULT 1,
    UNIQUE(series_id, season_number)
);

CREATE TABLE episodes (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id      INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_number  INTEGER NOT NULL,
    episode_number INTEGER NOT NULL,
    tmdb_id        INTEGER NOT NULL DEFAULT 0,
    title          TEXT NOT NULL DEFAULT '',
    overview       TEXT NOT NULL DEFAULT '',
    air_date       TEXT NOT NULL DEFAULT '',
    monitored      INTEGER NOT NULL DEFAULT 1,
    UNIQUE(series_id, season_number, episode_number)
);

CREATE TABLE movies (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id            INTEGER NOT NULL UNIQUE,
    title              TEXT NOT NULL,
    sort_title         TEXT NOT NULL DEFAULT '',
    overview           TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT '',
    year               INTEGER NOT NULL DEFAULT 0,
    release_date       TEXT NOT NULL DEFAULT '',
    runtime            INTEGER NOT NULL DEFAULT 0,
    imdb_id            TEXT NOT NULL DEFAULT '',
    poster_url         TEXT NOT NULL DEFAULT '',
    fanart_url         TEXT NOT NULL DEFAULT '',
    root_folder_id     INTEGER REFERENCES root_folders(id),
    quality_profile_id INTEGER,
    monitored          INTEGER NOT NULL DEFAULT 1,
    added_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_refreshed_at  DATETIME
);
