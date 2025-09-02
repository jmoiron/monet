package uploads

import (
	"fmt"

	"github.com/jmoiron/monet/db"
)

// UploadService handles database operations for uploads
type UploadService struct {
	db db.DB
}

// NewUploadService creates a new uploads service
func NewUploadService(database db.DB) *UploadService {
	return &UploadService{db: database}
}

// Create inserts a new upload record into the database
func (s *UploadService) Create(filesystemName, filename string, size int64) (*Upload, error) {
	upload := &Upload{
		FilesystemName: filesystemName,
		Filename:       filename,
		Size:           size,
	}

	query := `INSERT INTO upload (filesystem_name, filename, size) VALUES (?, ?, ?) RETURNING id, created_at`
	err := s.db.Get(upload, query, upload.FilesystemName, upload.Filename, upload.Size)
	if err != nil {
		return nil, fmt.Errorf("failed to create upload record: %w", err)
	}

	return upload, nil
}

// GetByID retrieves an upload by its ID
func (s *UploadService) GetByID(id uint64) (*Upload, error) {
	var upload Upload
	query := `SELECT id, filesystem_name, filename, size, created_at FROM upload WHERE id = ?`
	err := s.db.Get(&upload, query, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get upload by ID %d: %w", id, err)
	}
	return &upload, nil
}

// GetByFilename retrieves an upload by filesystem name and filename
func (s *UploadService) GetByFilename(filesystemName, filename string) (*Upload, error) {
	var upload Upload
	query := `SELECT id, filesystem_name, filename, size, created_at FROM upload
			  WHERE filesystem_name = ? AND filename = ?`
	err := s.db.Get(&upload, query, filesystemName, filename)
	if err != nil {
		return nil, fmt.Errorf("failed to get upload by filename %s/%s: %w", filesystemName, filename, err)
	}
	return &upload, nil
}

// List retrieves all uploads, optionally filtered by filesystem name
func (s *UploadService) List(filesystemName string, limit, offset int) ([]*Upload, error) {
	var uploads []*Upload
	var query string
	var args []any

	if filesystemName != "" {
		query = `SELECT id, filesystem_name, filename, size, created_at FROM upload
				 WHERE filesystem_name = ?
				 ORDER BY created_at DESC LIMIT ? OFFSET ?`
		args = []any{filesystemName, limit, offset}
	} else {
		query = `SELECT id, filesystem_name, filename, size, created_at FROM upload
				 ORDER BY created_at DESC LIMIT ? OFFSET ?`
		args = []any{limit, offset}
	}

	err := s.db.Select(&uploads, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list uploads: %w", err)
	}

	return uploads, nil
}

// Count returns the total number of uploads, optionally filtered by filesystem name
func (s *UploadService) Count(filesystemName string) (int, error) {
	var count int
	var query string
	var args []any

	if filesystemName != "" {
		query = `SELECT COUNT(*) FROM upload WHERE filesystem_name = ?`
		args = []any{filesystemName}
	} else {
		query = `SELECT COUNT(*) FROM upload`
		args = []any{}
	}

	err := s.db.Get(&count, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to count uploads: %w", err)
	}

	return count, nil
}

// Delete removes an upload record from the database by ID
func (s *UploadService) Delete(id uint64) error {
	query := `DELETE FROM upload WHERE id = ?`
	result, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete upload with ID %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("upload with ID %d not found", id)
	}

	return nil
}

// DeleteByFilename removes an upload record from the database by filesystem name and filename
func (s *UploadService) DeleteByFilename(filesystemName, filename string) error {
	query := `DELETE FROM upload WHERE filesystem_name = ? AND filename = ?`
	result, err := s.db.Exec(query, filesystemName, filename)
	if err != nil {
		return fmt.Errorf("failed to delete upload %s/%s: %w", filesystemName, filename, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("upload %s/%s not found", filesystemName, filename)
	}

	return nil
}

// UpdateFilename updates the filename of an upload record
func (s *UploadService) UpdateFilename(id uint64, newFilename string) error {
	query := `UPDATE upload SET filename = ? WHERE id = ?`
	result, err := s.db.Exec(query, newFilename, id)
	if err != nil {
		return fmt.Errorf("failed to update filename for upload ID %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("upload with ID %d not found", id)
	}

	return nil
}
