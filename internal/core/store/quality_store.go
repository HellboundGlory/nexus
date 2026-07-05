package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ErrProfileInUse is returned by DeleteQualityProfile when a series or movie
// still references the profile.
var ErrProfileInUse = errors.New("store: quality profile in use")

// QualityProfileItem is one quality's membership/ordering in a profile.
type QualityProfileItem struct {
	QualityID int  `json:"qualityId"`
	Allowed   bool `json:"allowed"`
}

// QualityProfile is a user-defined quality selection. Items is stored as a
// JSON array capturing both the allowed set and the ordering.
type QualityProfile struct {
	ID              int64                `json:"id"`
	Name            string               `json:"name"`
	CutoffQualityID int                  `json:"cutoffQualityId"`
	UpgradeAllowed  bool                 `json:"upgradeAllowed"`
	Items           []QualityProfileItem `json:"items"`
	CreatedAt       time.Time            `json:"createdAt"`
}

func (s *Store) CreateQualityProfile(ctx context.Context, p QualityProfile) (QualityProfile, error) {
	itemsJSON, err := json.Marshal(p.Items)
	if err != nil {
		return QualityProfile{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO quality_profiles (name, cutoff_quality_id, upgrade_allowed, items) VALUES (?, ?, ?, ?)`,
		p.Name, p.CutoffQualityID, boolToInt(p.UpgradeAllowed), string(itemsJSON))
	if err != nil {
		return QualityProfile{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetQualityProfile(ctx, id)
}

func (s *Store) GetQualityProfile(ctx context.Context, id int64) (QualityProfile, error) {
	var (
		p         QualityProfile
		upgrade   int
		itemsJSON string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, cutoff_quality_id, upgrade_allowed, items, created_at FROM quality_profiles WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.CutoffQualityID, &upgrade, &itemsJSON, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return QualityProfile{}, ErrNotFound
	}
	if err != nil {
		return QualityProfile{}, err
	}
	p.UpgradeAllowed = upgrade != 0
	if err := json.Unmarshal([]byte(itemsJSON), &p.Items); err != nil {
		return QualityProfile{}, err
	}
	return p, nil
}

func (s *Store) ListQualityProfiles(ctx context.Context) ([]QualityProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, cutoff_quality_id, upgrade_allowed, items, created_at FROM quality_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QualityProfile
	for rows.Next() {
		var (
			p         QualityProfile
			upgrade   int
			itemsJSON string
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.CutoffQualityID, &upgrade, &itemsJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.UpgradeAllowed = upgrade != 0
		if err := json.Unmarshal([]byte(itemsJSON), &p.Items); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateQualityProfile(ctx context.Context, p QualityProfile) error {
	itemsJSON, err := json.Marshal(p.Items)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE quality_profiles SET name = ?, cutoff_quality_id = ?, upgrade_allowed = ?, items = ? WHERE id = ?`,
		p.Name, p.CutoffQualityID, boolToInt(p.UpgradeAllowed), string(itemsJSON), p.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteQualityProfile(ctx context.Context, id int64) error {
	var refs int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM series WHERE quality_profile_id = ?) +
		        (SELECT COUNT(*) FROM movies WHERE quality_profile_id = ?)`, id, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrProfileInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM quality_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetSeriesQualityProfileID sets or clears a series' quality profile (nil clears).
func (s *Store) SetSeriesQualityProfileID(ctx context.Context, seriesID int64, profileID *int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE series SET quality_profile_id = ? WHERE id = ?`, profileID, seriesID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetMovieQualityProfileID sets or clears a movie's quality profile (nil clears).
func (s *Store) SetMovieQualityProfileID(ctx context.Context, movieID int64, profileID *int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE movies SET quality_profile_id = ? WHERE id = ?`, profileID, movieID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
