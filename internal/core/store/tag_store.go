package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrTagExists is returned when a label collides with an existing tag,
// case-insensitively.
var ErrTagExists = errors.New("store: tag label already exists")

// ErrInvalidTag is returned for a label that is empty after trimming.
var ErrInvalidTag = errors.New("store: invalid tag label")

// ErrTagNotFound is returned when an association write names a tag id that does
// not exist. Distinct from ErrNotFound, which means the series/movie/tag being
// addressed is missing.
var ErrTagNotFound = errors.New("store: tag not found")

// ErrTagInUse is the sentinel for a refused delete. The concrete error returned
// is *TagInUseError, which carries the counts; errors.Is matches this sentinel.
var ErrTagInUse = errors.New("store: tag in use")

// TagInUseError reports how many series and movies still reference a tag, so
// the API can name them in the 409 without a second query.
type TagInUseError struct {
	SeriesCount int
	MovieCount  int
}

func (e *TagInUseError) Error() string {
	return fmt.Sprintf("store: tag in use by %d series and %d movies", e.SeriesCount, e.MovieCount)
}

func (e *TagInUseError) Is(target error) bool { return target == ErrTagInUse }

// Tag is a user-defined label. SeriesCount and MovieCount are populated by
// ListTags only; CreateTag returns zeroes because a new tag has no associations.
type Tag struct {
	ID          int64  `json:"id"`
	Label       string `json:"label"`
	SeriesCount int    `json:"seriesCount"`
	MovieCount  int    `json:"movieCount"`
}

func normalizeLabel(label string) (string, error) {
	l := strings.TrimSpace(label)
	if l == "" {
		return "", ErrInvalidTag
	}
	return l, nil
}

// isUniqueViolation reports whether err is a UNIQUE constraint failure. The
// modernc driver does not export a typed error for this, so the message is the
// only signal available.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(strings.ToUpper(err.Error()), "UNIQUE CONSTRAINT FAILED")
}

func (s *Store) CreateTag(ctx context.Context, label string) (Tag, error) {
	l, err := normalizeLabel(label)
	if err != nil {
		return Tag{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO tags (label) VALUES (?)`, l)
	if isUniqueViolation(err) {
		return Tag{}, ErrTagExists
	}
	if err != nil {
		return Tag{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Tag{}, err
	}
	return Tag{ID: id, Label: l}, nil
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.label,
		        (SELECT COUNT(*) FROM series_tags st WHERE st.tag_id = t.id),
		        (SELECT COUNT(*) FROM movie_tags  mt WHERE mt.tag_id = t.id)
		 FROM tags t ORDER BY t.label COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var tg Tag
		if err := rows.Scan(&tg.ID, &tg.Label, &tg.SeriesCount, &tg.MovieCount); err != nil {
			return nil, err
		}
		out = append(out, tg)
	}
	return out, rows.Err()
}

func (s *Store) RenameTag(ctx context.Context, id int64, label string) error {
	l, err := normalizeLabel(label)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tags SET label = ? WHERE id = ?`, l, id)
	if isUniqueViolation(err) {
		return ErrTagExists
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	var seriesCount, movieCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM series_tags WHERE tag_id = ?),
		        (SELECT COUNT(*) FROM movie_tags  WHERE tag_id = ?)`, id, id).
		Scan(&seriesCount, &movieCount)
	if err != nil {
		return err
	}
	if seriesCount > 0 || movieCount > 0 {
		return &TagInUseError{SeriesCount: seriesCount, MovieCount: movieCount}
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
