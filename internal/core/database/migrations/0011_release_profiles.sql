CREATE TABLE release_profiles (
  id            INTEGER PRIMARY KEY,
  name          TEXT NOT NULL,
  required_mode TEXT NOT NULL DEFAULT 'any',
  required_any  TEXT NOT NULL DEFAULT '[]',
  required_all  TEXT NOT NULL DEFAULT '[]',
  ignored       TEXT NOT NULL DEFAULT '[]',
  preferred     TEXT NOT NULL DEFAULT '[]',
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE release_profile_tags (
  release_profile_id INTEGER NOT NULL REFERENCES release_profiles(id) ON DELETE CASCADE,
  tag_id             INTEGER NOT NULL REFERENCES tags(id)             ON DELETE CASCADE,
  PRIMARY KEY(release_profile_id, tag_id)
);

CREATE INDEX idx_release_profile_tags_tag ON release_profile_tags(tag_id);