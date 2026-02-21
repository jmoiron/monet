package autosave

import "time"

// Autosave represents an automatically saved version of content
type Autosave struct {
	ID          int       `db:"id" json:"id"`
	ContentType string    `db:"content_type" json:"content_type"`
	ContentID   int       `db:"content_id" json:"content_id"`
	Content     string    `db:"content" json:"content"`
	Title       string    `db:"title" json:"title"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
}

// AutosaveWithDiff is an Autosave with a unified diff pre-computed against
// the current saved content of the parent object.
type AutosaveWithDiff struct {
	Autosave
	Diff string `json:"diff"`
}
