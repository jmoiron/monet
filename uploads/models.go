package uploads

import (
	"time"

	"github.com/jmoiron/monet/db/monarch"
)

var uploadMigrations = monarch.Set{
	Name: "uploads",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS upload (
				id INTEGER PRIMARY KEY,
				filesystem_name TEXT NOT NULL,
				filename TEXT NOT NULL,
				size INTEGER DEFAULT 0,
				created_at datetime DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE upload;`,
		}, {
			Up:   `CREATE INDEX IF NOT EXISTS idx_upload_filesystem ON upload(filesystem_name);`,
			Down: `DROP INDEX idx_upload_filesystem;`,
		}, {
			Up:   `CREATE INDEX IF NOT EXISTS idx_upload_created_at ON upload(created_at);`,
			Down: `DROP INDEX idx_upload_created_at;`,
		},
	},
}

// Upload represents a file upload tracked in the database
type Upload struct {
	ID             uint64    `db:"id" json:"id"`
	FilesystemName string    `db:"filesystem_name" json:"filesystem_name"`
	Filename       string    `db:"filename" json:"filename"`
	Size           int64     `db:"size" json:"size"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	URL            string    `db:"-" json:"url"` // Not stored in DB, populated when needed
}
