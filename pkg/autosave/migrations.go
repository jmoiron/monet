package autosave

import "github.com/jmoiron/monet/db/monarch"

// Migrations returns the database migrations for the autosave system
func Migrations() monarch.Set {
	return monarch.Set{
		Name: "autosave",
		Migrations: []monarch.Migration{
			{
				Up: `CREATE TABLE IF NOT EXISTS autosaves (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					content_type TEXT NOT NULL,
					content_id INTEGER NOT NULL,
					content TEXT NOT NULL,
					title TEXT,
					created_at INTEGER NOT NULL
				)`,
				Down: `DROP TABLE autosaves`,
			},
			{
				Up:   `CREATE INDEX idx_autosaves_lookup ON autosaves(content_type, content_id, created_at DESC)`,
				Down: `DROP INDEX idx_autosaves_lookup`,
			},
			{
				Up: `CREATE TABLE autosaves_new (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					content_type TEXT NOT NULL,
					content_id INTEGER NOT NULL,
					content TEXT NOT NULL,
					title TEXT,
					created_at datetime NOT NULL
				);
				INSERT INTO autosaves_new SELECT id, content_type, content_id, content, title, datetime(created_at, 'unixepoch') FROM autosaves;
				DROP TABLE autosaves;
				ALTER TABLE autosaves_new RENAME TO autosaves;
				CREATE INDEX idx_autosaves_lookup ON autosaves(content_type, content_id, created_at DESC)`,
				Down: `CREATE TABLE autosaves_new (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					content_type TEXT NOT NULL,
					content_id INTEGER NOT NULL,
					content TEXT NOT NULL,
					title TEXT,
					created_at INTEGER NOT NULL
				);
				INSERT INTO autosaves_new SELECT id, content_type, content_id, content, title, strftime('%s', created_at) FROM autosaves;
				DROP TABLE autosaves;
				ALTER TABLE autosaves_new RENAME TO autosaves;
				CREATE INDEX idx_autosaves_lookup ON autosaves(content_type, content_id, created_at DESC)`,
			},
		},
	}
}
