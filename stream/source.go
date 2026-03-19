package stream

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
)

var sourceMigration = monarch.Set{
	Name: "stream_source",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS stream_source (
				id integer PRIMARY KEY,
				kind text NOT NULL,
				name text NOT NULL,
				enabled integer NOT NULL DEFAULT 0,
				schedule_minutes integer NOT NULL DEFAULT 60,
				settings_json text NOT NULL DEFAULT '{}',
				last_run_at integer NOT NULL DEFAULT 0,
				last_success_at integer NOT NULL DEFAULT 0,
				last_error text NOT NULL DEFAULT '',
				created_at integer NOT NULL,
				updated_at integer NOT NULL
			);`,
			Down: `DROP TABLE stream_source;`,
		}, {
			Up:   `CREATE UNIQUE INDEX IF NOT EXISTS stream_source_kind ON stream_source (kind);`,
			Down: `DROP INDEX IF EXISTS stream_source_kind;`,
		}, {
			Up: `CREATE TABLE IF NOT EXISTS stream_run (
				id integer PRIMARY KEY,
				source_id integer NOT NULL,
				status text NOT NULL,
				started_at integer NOT NULL,
				finished_at integer NOT NULL DEFAULT 0,
				imported_count integer NOT NULL DEFAULT 0,
				deleted_count integer NOT NULL DEFAULT 0,
				error text NOT NULL DEFAULT '',
				details_json text NOT NULL DEFAULT '{}'
			);`,
			Down: `DROP TABLE stream_run;`,
		}, {
			Up:   `CREATE INDEX IF NOT EXISTS stream_run_source_started ON stream_run (source_id, started_at DESC);`,
			Down: `DROP INDEX IF EXISTS stream_run_source_started;`,
		},
	},
}

type StreamSource struct {
	ID              int64  `db:"id"`
	Kind            string `db:"kind"`
	Name            string `db:"name"`
	Enabled         bool   `db:"enabled"`
	ScheduleMinutes int    `db:"schedule_minutes"`
	SettingsJSON    string `db:"settings_json"`
	LastRunAt       int64  `db:"last_run_at"`
	LastSuccessAt   int64  `db:"last_success_at"`
	LastError       string `db:"last_error"`
	CreatedAt       int64  `db:"created_at"`
	UpdatedAt       int64  `db:"updated_at"`
}

func (s *StreamSource) Settings() map[string]string {
	settings := map[string]string{}
	if len(s.SettingsJSON) == 0 {
		return settings
	}
	_ = json.Unmarshal([]byte(s.SettingsJSON), &settings)
	return settings
}

func (s *StreamSource) SetSettings(settings map[string]string) error {
	if settings == nil {
		settings = map[string]string{}
	}
	buf, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	s.SettingsJSON = string(buf)
	return nil
}

func (s *StreamSource) LastRunTime() time.Time {
	if s.LastRunAt == 0 {
		return time.Time{}
	}
	return time.Unix(s.LastRunAt, 0)
}

func (s *StreamSource) LastSuccessTime() time.Time {
	if s.LastSuccessAt == 0 {
		return time.Time{}
	}
	return time.Unix(s.LastSuccessAt, 0)
}

type StreamRun struct {
	ID            int64  `db:"id"`
	SourceID      int64  `db:"source_id"`
	Status        string `db:"status"`
	StartedAt     int64  `db:"started_at"`
	FinishedAt    int64  `db:"finished_at"`
	ImportedCount int    `db:"imported_count"`
	DeletedCount  int    `db:"deleted_count"`
	Error         string `db:"error"`
	DetailsJSON   string `db:"details_json"`
}

func (r *StreamRun) Details() map[string]any {
	details := map[string]any{}
	if len(r.DetailsJSON) == 0 {
		return details
	}
	_ = json.Unmarshal([]byte(r.DetailsJSON), &details)
	return details
}

type SourceService struct {
	db db.DB
}

func NewSourceService(db db.DB) *SourceService {
	return &SourceService{db: db}
}

func (s *SourceService) EnsureDefaults(modules []Module) error {
	now := time.Now().Unix()
	for _, module := range modules {
		var count int
		if err := s.db.Get(&count, `SELECT count(*) FROM stream_source WHERE kind=?`, module.Kind()); err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		source := StreamSource{
			Kind:            module.Kind(),
			Name:            module.Name(),
			Enabled:         false,
			ScheduleMinutes: module.DefaultScheduleMinutes(),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := source.SetSettings(module.DefaultSettings()); err != nil {
			return err
		}

		stmt, err := s.db.PrepareNamed(`INSERT INTO stream_source
			(kind, name, enabled, schedule_minutes, settings_json, last_run_at, last_success_at, last_error, created_at, updated_at)
			VALUES (:kind, :name, :enabled, :schedule_minutes, :settings_json, :last_run_at, :last_success_at, :last_error, :created_at, :updated_at)`)
		if err != nil {
			return err
		}
		if _, err := stmt.Exec(&source); err != nil {
			return err
		}
	}
	return nil
}

func (s *SourceService) List() ([]*StreamSource, error) {
	var sources []*StreamSource
	err := s.db.Select(&sources, `SELECT * FROM stream_source ORDER BY name ASC`)
	return sources, err
}

func (s *SourceService) GetByKind(kind string) (*StreamSource, error) {
	var source StreamSource
	if err := s.db.Get(&source, `SELECT * FROM stream_source WHERE kind=?`, kind); err != nil {
		return nil, err
	}
	return &source, nil
}

func (s *SourceService) Save(source *StreamSource) error {
	source.UpdatedAt = time.Now().Unix()
	stmt, err := s.db.PrepareNamed(`UPDATE stream_source SET
		name=:name,
		enabled=:enabled,
		schedule_minutes=:schedule_minutes,
		settings_json=:settings_json,
		updated_at=:updated_at
		WHERE id=:id`)
	if err != nil {
		return err
	}
	_, err = stmt.Exec(source)
	return err
}

func (s *SourceService) MarkRunStart(sourceID int64) error {
	_, err := s.db.Exec(`UPDATE stream_source SET last_run_at=?, updated_at=? WHERE id=?`, time.Now().Unix(), time.Now().Unix(), sourceID)
	return err
}

func (s *SourceService) MarkRunResult(sourceID int64, run *RunResult, runErr error) error {
	now := time.Now().Unix()
	lastError := ""
	lastSuccessAt := int64(0)
	if runErr == nil {
		lastSuccessAt = now
	} else {
		lastError = runErr.Error()
	}

	_, err := s.db.Exec(`UPDATE stream_source
		SET last_success_at=CASE WHEN ? > 0 THEN ? ELSE last_success_at END,
			last_error=?,
			updated_at=?
		WHERE id=?`,
		lastSuccessAt, lastSuccessAt, lastError, now, sourceID)
	return err
}

func (s *SourceService) CreateRun(sourceID int64) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(`INSERT INTO stream_run
		(source_id, status, started_at, finished_at, imported_count, deleted_count, error, details_json)
		VALUES (?, 'running', ?, 0, 0, 0, '', '{}')`, sourceID, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SourceService) FinishRun(runID int64, result *RunResult, runErr error) error {
	if result == nil {
		result = &RunResult{}
	}
	detailsJSON := "{}"
	if len(result.Details) > 0 {
		buf, err := json.Marshal(result.Details)
		if err != nil {
			return err
		}
		detailsJSON = string(buf)
	}

	status := "success"
	errText := ""
	if runErr != nil {
		status = "error"
		errText = runErr.Error()
	}

	_, err := s.db.Exec(`UPDATE stream_run SET
		status=?,
		finished_at=?,
		imported_count=?,
		deleted_count=?,
		error=?,
		details_json=?
		WHERE id=?`,
		status,
		time.Now().Unix(),
		result.Imported,
		result.Deleted,
		errText,
		detailsJSON,
		runID,
	)
	return err
}

func (s *SourceService) LatestRuns(limit int) ([]*StreamRun, error) {
	if limit <= 0 {
		limit = 10
	}
	var runs []*StreamRun
	err := s.db.Select(&runs, fmt.Sprintf(`SELECT * FROM stream_run ORDER BY started_at DESC LIMIT %d`, limit))
	return runs, err
}
