package autosave

import (
	"fmt"
	"time"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"github.com/jmoiron/monet/db"
)

// Service handles autosave operations
type Service struct {
	db db.DB
}

// NewService creates a new autosave service
func NewService(database db.DB) *Service {
	return &Service{db: database}
}

// Save creates a new autosave and removes old versions if necessary
func (s *Service) Save(contentType string, contentID int, content, title string) error {
	// Insert the new autosave
	_, err := s.db.Exec(`
		INSERT INTO autosaves (content_type, content_id, content, title, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		contentType, contentID, content, title, time.Now())
	if err != nil {
		return err
	}

	// Delete old versions, keeping only the most recent maxVersions
	return s.DeleteOldVersions(contentType, contentID, 10)
}

// List returns all autosaves for a specific piece of content, ordered by created_at DESC
func (s *Service) List(contentType string, contentID int) ([]Autosave, error) {
	var autosaves []Autosave
	err := s.db.Select(&autosaves, `
		SELECT id, content_type, content_id, content, title, created_at
		FROM autosaves
		WHERE content_type = ? AND content_id = ?
		ORDER BY created_at DESC`,
		contentType, contentID)
	return autosaves, err
}

// Get retrieves a specific autosave by ID
func (s *Service) Get(id int) (*Autosave, error) {
	var autosave Autosave
	err := s.db.Get(&autosave, `
		SELECT id, content_type, content_id, content, title, created_at
		FROM autosaves
		WHERE id = ?`,
		id)
	if err != nil {
		return nil, err
	}
	return &autosave, nil
}

// LoadWithDiffs returns all autosaves for a piece of content with a unified diff
// pre-computed for each against savedContent (the current saved state of the parent).
func (s *Service) LoadWithDiffs(contentType string, contentID int, savedContent string) ([]AutosaveWithDiff, error) {
	autosaves, err := s.List(contentType, contentID)
	if err != nil {
		return nil, err
	}
	result := make([]AutosaveWithDiff, len(autosaves))
	for i, as := range autosaves {
		edits := myers.ComputeEdits(span.URIFromPath("saved"), savedContent, as.Content)
		result[i] = AutosaveWithDiff{
			Autosave: as,
			Diff:     fmt.Sprint(gotextdiff.ToUnified("saved", "autosave", savedContent, edits)),
		}
	}
	return result, nil
}

// Delete removes a single autosave by ID.
func (s *Service) Delete(id int) error {
	_, err := s.db.Exec(`DELETE FROM autosaves WHERE id = ?`, id)
	return err
}

// DeleteOldVersions removes old autosaves, keeping only the most recent keepCount versions
func (s *Service) DeleteOldVersions(contentType string, contentID int, keepCount int) error {
	_, err := s.db.Exec(`
		DELETE FROM autosaves
		WHERE content_type = ? AND content_id = ?
		AND id NOT IN (
			SELECT id FROM autosaves
			WHERE content_type = ? AND content_id = ?
			ORDER BY created_at DESC
			LIMIT ?
		)`,
		contentType, contentID, contentType, contentID, keepCount)
	return err
}
