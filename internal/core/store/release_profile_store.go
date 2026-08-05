package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ReleaseProfile is a reusable named rule scoped to media by tag. Term lists
// are matched case-insensitively as substrings on the raw release title.
type ReleaseProfile struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	RequiredMode string    `json:"requiredMode"`
	RequiredAny  []string  `json:"requiredAny"`
	RequiredAll  []string  `json:"requiredAll"`
	Ignored      []string  `json:"ignored"`
	Preferred    []string  `json:"preferred"`
	TagIDs       []int64   `json:"tagIds"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (s *Store) CreateReleaseProfile(ctx context.Context, p ReleaseProfile) (ReleaseProfile, error) {
	anyJSON, err := json.Marshal(p.RequiredAny)
	if err != nil {
		return ReleaseProfile{}, err
	}
	allJSON, err := json.Marshal(p.RequiredAll)
	if err != nil {
		return ReleaseProfile{}, err
	}
	ignJSON, err := json.Marshal(p.Ignored)
	if err != nil {
		return ReleaseProfile{}, err
	}
	prefJSON, err := json.Marshal(p.Preferred)
	if err != nil {
		return ReleaseProfile{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReleaseProfile{}, err
	}
	defer tx.Rollback()
	if err := s.checkTagIDs(ctx, tx, p.TagIDs); err != nil {
		return ReleaseProfile{}, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO release_profiles (name, required_mode, required_any, required_all, ignored, preferred) VALUES (?, ?, ?, ?, ?, ?)`,
		p.Name, p.RequiredMode, string(anyJSON), string(allJSON), string(ignJSON), string(prefJSON))
	if err != nil {
		return ReleaseProfile{}, err
	}
	id, _ := res.LastInsertId()
	for _, tagID := range p.TagIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO release_profile_tags (release_profile_id, tag_id) VALUES (?, ?)`, id, tagID); err != nil {
			return ReleaseProfile{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ReleaseProfile{}, err
	}
	return s.GetReleaseProfile(ctx, id)
}

func (s *Store) GetReleaseProfile(ctx context.Context, id int64) (ReleaseProfile, error) {
	var (
		p        ReleaseProfile
		anyJSON  string
		allJSON  string
		ignJSON  string
		prefJSON string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, required_mode, required_any, required_all, ignored, preferred, created_at FROM release_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.RequiredMode, &anyJSON, &allJSON, &ignJSON, &prefJSON, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ReleaseProfile{}, ErrNotFound
	}
	if err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(anyJSON), &p.RequiredAny); err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(allJSON), &p.RequiredAll); err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(ignJSON), &p.Ignored); err != nil {
		return ReleaseProfile{}, err
	}
	if err := json.Unmarshal([]byte(prefJSON), &p.Preferred); err != nil {
		return ReleaseProfile{}, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT tag_id FROM release_profile_tags WHERE release_profile_id = ? ORDER BY tag_id`, id)
	if err != nil {
		return ReleaseProfile{}, err
	}
	defer rows.Close()
	p.TagIDs = []int64{}
	for rows.Next() {
		var tagID int64
		if err := rows.Scan(&tagID); err != nil {
			return ReleaseProfile{}, err
		}
		p.TagIDs = append(p.TagIDs, tagID)
	}
	return p, rows.Err()
}

func (s *Store) ListReleaseProfiles(ctx context.Context) ([]ReleaseProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, required_mode, required_any, required_all, ignored, preferred, created_at FROM release_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReleaseProfile
	for rows.Next() {
		var (
			p        ReleaseProfile
			anyJSON  string
			allJSON  string
			ignJSON  string
			prefJSON string
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.RequiredMode, &anyJSON, &allJSON, &ignJSON, &prefJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(anyJSON), &p.RequiredAny); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(allJSON), &p.RequiredAll); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(ignJSON), &p.Ignored); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(prefJSON), &p.Preferred); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Populate TagIDs for every profile in one query.
	tagRows, err := s.db.QueryContext(ctx,
		`SELECT release_profile_id, tag_id FROM release_profile_tags ORDER BY release_profile_id, tag_id`)
	if err != nil {
		return nil, err
	}
	defer tagRows.Close()
	byID := map[int64]*ReleaseProfile{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for tagRows.Next() {
		var profileID, tagID int64
		if err := tagRows.Scan(&profileID, &tagID); err != nil {
			return nil, err
		}
		if p, ok := byID[profileID]; ok {
			p.TagIDs = append(p.TagIDs, tagID)
		}
	}
	if err := tagRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].TagIDs == nil {
			out[i].TagIDs = []int64{}
		}
	}
	return out, nil
}

func (s *Store) UpdateReleaseProfile(ctx context.Context, p ReleaseProfile) error {
	anyJSON, err := json.Marshal(p.RequiredAny)
	if err != nil {
		return err
	}
	allJSON, err := json.Marshal(p.RequiredAll)
	if err != nil {
		return err
	}
	ignJSON, err := json.Marshal(p.Ignored)
	if err != nil {
		return err
	}
	prefJSON, err := json.Marshal(p.Preferred)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.checkTagIDs(ctx, tx, p.TagIDs); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE release_profiles SET name = ?, required_mode = ?, required_any = ?, required_all = ?, ignored = ?, preferred = ? WHERE id = ?`,
		p.Name, p.RequiredMode, string(anyJSON), string(allJSON), string(ignJSON), string(prefJSON), p.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM release_profile_tags WHERE release_profile_id = ?`, p.ID); err != nil {
		return err
	}
	for _, tagID := range p.TagIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO release_profile_tags (release_profile_id, tag_id) VALUES (?, ?)`, p.ID, tagID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteReleaseProfile(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM release_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// checkTagIDs verifies every tag id exists inside the transaction, so an
// unknown id returns ErrTagNotFound and rolls back (no partial write).
func (s *Store) checkTagIDs(ctx context.Context, tx *sql.Tx, tagIDs []int64) error {
	for _, tagID := range tagIDs {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrTagNotFound
		}
	}
	return nil
}

// SeriesReleaseProfileIDs returns every series' applicable release-profile ids
// in one query. Series with no applicable profiles are omitted. Built once per
// RSS sweep, not per release.
func (s *Store) SeriesReleaseProfileIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT st.series_id, rpt.release_profile_id
		 FROM series_tags st
		 JOIN release_profile_tags rpt ON rpt.tag_id = st.tag_id
		 ORDER BY st.series_id, rpt.release_profile_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var seriesID, profileID int64
		if err := rows.Scan(&seriesID, &profileID); err != nil {
			return nil, err
		}
		out[seriesID] = append(out[seriesID], profileID)
	}
	return out, rows.Err()
}

// MovieReleaseProfileIDs returns every movie's applicable release-profile ids
// in one query. Movies with no applicable profiles are omitted. Built once per
// RSS sweep, not per release.
func (s *Store) MovieReleaseProfileIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT mt.movie_id, rpt.release_profile_id
		 FROM movie_tags mt
		 JOIN release_profile_tags rpt ON rpt.tag_id = mt.tag_id
		 ORDER BY mt.movie_id, rpt.release_profile_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var movieID, profileID int64
		if err := rows.Scan(&movieID, &profileID); err != nil {
			return nil, err
		}
		out[movieID] = append(out[movieID], profileID)
	}
	return out, rows.Err()
}