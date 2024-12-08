package monarch

import (
	"fmt"
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
					down text NOT NULL,
					applied_at datetime NOT NULL,
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

func (m *Manager) LatestVersions() ([]MigrationVersion, error) {
	q := `WITH ranked AS (
		SELECT version, name, applied_at, rank() over (partition by name order by version desc) AS rank FROM migrations
	) SELECT version, name, applied_at FROM ranked WHERE rank=1 ORDER BY name;`

	var mvs []MigrationVersion
	err := m.db.Select(&mvs, q)
	return mvs, err
}

// Downgrade a single version
func (m *Manager) Downgrade(name string) error {
	var cur MigrationVersion
	q := `SELECT * FROM migrations WHERE name=? ORDER BY version DESC LIMIT 1;`
	if err := m.db.Get(&cur, q, name); err != nil {
		return err
	}

	if cur.Version == 0 {
		return fmt.Errorf("cannot downgrade past version 0")
	}

	_, err := m.db.Exec(cur.Down)
	if err != nil {
		return fmt.Errorf("executing `%s` (%w)", cur.Down, err)
	}

	if err := m.RemoveVersion(name, cur.Version); err != nil {
		return err
	}

	return nil
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
			return fmt.Errorf("'%s' version %d: %w <%s>", set.Name, v, err, mig.Up)
		}
		// this would be bad;  we've applied a migraiton safely
		// but could not update the version.
		err = m.AddVersion(set.Name, v, mig.Down)
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
func (m *Manager) AddVersion(setName string, version int, down string) error {
	now := time.Now().Unix()
	_, err := m.db.Exec(`INSERT INTO migrations (version, name, applied_at, down) VALUES (?, ?, ?, ?);`,
		version, setName, now, down)
	return err
}

func (m *Manager) RemoveVersion(setName string, version int) error {
	_, err := m.db.Exec(`DELETE FROM migrations WHERE name=? AND version=?;`, setName, version)
	return err
}

// A MigrationVersion contains information about a specific migration's application.
type MigrationVersion struct {
	Name      string
	Version   int
	Up        string
	Down      string
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
