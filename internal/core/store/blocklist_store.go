package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type Blocklist struct {
	ID          int64     `json:"id"`
	MediaKind   string    `json:"mediaKind"`
	MovieID     *int64    `json:"movieId,omitempty"`
	SeriesID    *int64    `json:"seriesId,omitempty"`
	SourceTitle string    `json:"sourceTitle"`
	NormTitle   string    `json:"-"`
	Protocol    string    `json:"protocol"`
	QualityID   int       `json:"qualityId"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"createdAt"`
}

// NormReleaseTitle lowercases and collapses runs of non-alphanumeric characters
// to single spaces, so blocklist matching is robust to punctuation differences.
func NormReleaseTitle(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Store) AddBlocklist(ctx context.Context, bl Blocklist) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO blocklist (media_kind, movie_id, series_id, source_title, norm_title, protocol, quality_id, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		bl.MediaKind, bl.MovieID, bl.SeriesID, bl.SourceTitle, NormReleaseTitle(bl.SourceTitle),
		bl.Protocol, bl.QualityID, bl.Reason)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const blocklistCols = `id, media_kind, movie_id, series_id, source_title, norm_title, protocol, quality_id, reason, created_at`

func scanBlocklistRow(row rowScanner) (Blocklist, error) {
	var bl Blocklist
	var movieID, seriesID sql.NullInt64
	if err := row.Scan(&bl.ID, &bl.MediaKind, &movieID, &seriesID, &bl.SourceTitle,
		&bl.NormTitle, &bl.Protocol, &bl.QualityID, &bl.Reason, &bl.CreatedAt); err != nil {
		return Blocklist{}, err
	}
	if movieID.Valid {
		bl.MovieID = &movieID.Int64
	}
	if seriesID.Valid {
		bl.SeriesID = &seriesID.Int64
	}
	return bl, nil
}

func (s *Store) ListBlocklist(ctx context.Context) ([]Blocklist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+blocklistCols+` FROM blocklist ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Blocklist
	for rows.Next() {
		bl, err := scanBlocklistRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, bl)
	}
	return out, rows.Err()
}

func (s *Store) RemoveBlocklist(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM blocklist WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) BlocklistedTitles(ctx context.Context, movieID, seriesID *int64) (map[string]bool, error) {
	out := map[string]bool{}
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case movieID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title FROM blocklist WHERE movie_id = ?`, *movieID)
	case seriesID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title FROM blocklist WHERE series_id = ?`, *seriesID)
	default:
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out[t] = true
	}
	return out, rows.Err()
}

// BlocklistedReasons is BlocklistedTitles carrying the reason text, for callers
// that must explain *why* a release is blocked rather than just drop it (the
// interactive search list). Scoping matches BlocklistedTitles exactly.
func (s *Store) BlocklistedReasons(ctx context.Context, movieID, seriesID *int64) (map[string]string, error) {
	out := map[string]string{}
	var (
		rows *sql.Rows
		err  error
	)
	switch {
	case movieID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title, reason FROM blocklist WHERE movie_id = ?`, *movieID)
	case seriesID != nil:
		rows, err = s.db.QueryContext(ctx, `SELECT norm_title, reason FROM blocklist WHERE series_id = ?`, *seriesID)
	default:
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t, reason string
		if err := rows.Scan(&t, &reason); err != nil {
			return nil, err
		}
		out[t] = reason
	}
	return out, rows.Err()
}
