package monarch

import (
	"time"

	"github.com/jmoiron/monet/db"
)

// Monarch is a simple migration application library.
// It manages itself with itself.

// A Manager applies migrations.
type Manager struct {
	db db.DB
}

// NewManager creates a new manager.  If it has not been bootstrapped on this db,
// then it is bootstrapped now.  If it fails to bootstrap, it won't work.
func NewManager(db db.DB) (*Manager, error) {
	manager := &Manager{db: db}
	return manager, manager.bootstrap()
}

func (m *Manager) bootstrapMigrations() []Migration {
	return []Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS migrations (
					version int NOT NULL,
					name text NOT NULL,
					applied_at int NOT NULL,
					PRIMARY KEY (version, name)
				);`,
			Down: `DROP TABLE migrations;`,
		},
		{
			Up:   `CREATE INDEX migration_name ON migrations (name, version);`,
			Down: `DROP INDEX migration_name;`,
		},
	}
}

func (m *Manager) bootstrap() error {
	migrations := m.bootstrapMigrations()

	_, err := m.db.Exec(migrations[0].Up)
	if err != nil {
		return err
	}

	return m.Upgrade(Set{Name: "monarch", Migrations: migrations})
}

// Upgrade app to the latest migration level.
func (m *Manager) Upgrade(set Set) error {
	version, err := m.GetVersion(set.Name)
	if err != nil {
		return err
	}

	for v, mig := range set.Migrations {
		// skip already applied migrations
		if v <= version {
			continue
		}
		_, err := m.db.Exec(mig.Up)
		if err != nil {
			return err
		}
		// this would be bad;  we've applied a migraiton safely
		// but could not update the version.
		err = m.SetVersion(set.Name, v)
		if err != nil {
			return err
		}

	}
	return nil
}

// GetVersion returns the latest applied migration version for appName.
// If no version has been recorded in the migrations table, -1 is returned.
func (m *Manager) GetVersion(setName string) (version int, err error) {
	err = m.db.Get(&version, `SELECT COALESCE(max(version), -1)
	FROM migrations WHERE name=?;`, setName)
	return version, err
}

// SetVersion sets the version of setName to version.
func (m *Manager) SetVersion(setName string, version int) error {
	now := time.Now().Unix()
	_, err := m.db.Exec(`INSERT INTO migrations (version, name, applied_at) VALUES (?, ?, ?);`,
		version, setName, now)
	return err
}

// A MigrationVersion contains information about a specific migration's application.
type MigrationVersion struct {
	Name      string
	Version   int
	AppliedAt time.Time `db:"applied_at"`
}

// A Set is a named set of migrations.
type Set struct {
	Name       string
	Migrations []Migration
}

// A Migration is two statements;  one that, when executed, upgrades to that version,
// and another that can undo this (either by dropping columns, tables, etc).
type Migration struct {
	Up   string
	Down string
}
