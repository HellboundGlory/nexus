CREATE TABLE series_aliases (
  id         INTEGER PRIMARY KEY,
  series_id  INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  country    TEXT NOT NULL DEFAULT '',
  type       TEXT NOT NULL DEFAULT '',
  UNIQUE(series_id, title)
);
CREATE INDEX idx_series_aliases_series ON series_aliases(series_id);
