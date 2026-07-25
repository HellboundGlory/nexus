CREATE TABLE tags (
  id    INTEGER PRIMARY KEY,
  label TEXT NOT NULL,
  UNIQUE(label COLLATE NOCASE)
);

CREATE TABLE series_tags (
  series_id INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  tag_id    INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
  PRIMARY KEY(series_id, tag_id)
);

CREATE TABLE movie_tags (
  movie_id INTEGER NOT NULL REFERENCES movies(id) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
  PRIMARY KEY(movie_id, tag_id)
);

CREATE INDEX idx_series_tags_tag ON series_tags(tag_id);
CREATE INDEX idx_movie_tags_tag  ON movie_tags(tag_id);
