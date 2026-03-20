package stream

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/stream/sources"
)

type Runner struct {
	db      db.DB
	sources *SourceService
	events  *EventService
	modules *ModuleRegistry

	startOnce sync.Once

	mu      sync.Mutex
	running map[string]bool
}

func NewRunner(database db.DB, modules *ModuleRegistry) *Runner {
	return &Runner{
		db:      database,
		sources: NewSourceService(database),
		events:  NewEventService(database),
		modules: modules,
		running: map[string]bool{},
	}
}

func (r *Runner) Start() {
	r.startOnce.Do(func() {
		go r.loop()
	})
}

func (r *Runner) loop() {
	r.runDue(context.Background())

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		r.runDue(context.Background())
	}
}

func (r *Runner) runDue(ctx context.Context) {
	sources, err := r.sources.List()
	if err != nil {
		slog.Error("loading stream sources", "err", err)
		return
	}

	now := time.Now().Unix()
	for _, source := range sources {
		if !source.Enabled || source.ScheduleMinutes <= 0 {
			continue
		}
		if source.LastRunAt > 0 && now-source.LastRunAt < int64(source.ScheduleMinutes*60) {
			continue
		}

		go func(kind string) {
			if err := r.Run(context.Background(), kind); err != nil {
				slog.Error("running stream source", "kind", kind, "err", err)
			}
		}(source.Kind)
	}
}

func (r *Runner) IsRunning(kind string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[kind]
}

func (r *Runner) begin(kind string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[kind] {
		return false
	}
	r.running[kind] = true
	return true
}

func (r *Runner) end(kind string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, kind)
}

func (r *Runner) Run(ctx context.Context, kind string) error {
	module, ok := r.modules.Get(kind)
	if !ok {
		return fmt.Errorf("unknown source kind %q", kind)
	}

	if !r.begin(kind) {
		return fmt.Errorf("%s is already running", kind)
	}
	defer r.end(kind)

	source, err := r.sources.GetByKind(kind)
	if err != nil {
		return err
	}
	slog.Info("starting stream source run", "kind", kind, "source_id", source.ID, "enabled", source.Enabled, "schedule_minutes", source.ScheduleMinutes)

	if err := r.sources.MarkRunStart(source.ID); err != nil {
		return err
	}

	runID, err := r.sources.CreateRun(source.ID)
	if err != nil {
		return err
	}
	slog.Info("created stream run", "kind", kind, "source_id", source.ID, "run_id", runID)

	result, runErr := module.Sync(ctx, source)
	if runErr == nil {
		runErr = r.applyResult(module, source, result)
	}
	if finishErr := r.sources.FinishRun(runID, result, runErr); finishErr != nil {
		slog.Error("finishing stream run", "kind", kind, "err", finishErr)
	}
	if markErr := r.sources.MarkRunResult(source.ID, result, runErr); markErr != nil {
		slog.Error("marking stream run result", "kind", kind, "err", markErr)
	}
	if runErr != nil {
		slog.Error("stream source run failed", "kind", kind, "source_id", source.ID, "run_id", runID, "err", runErr)
		return runErr
	}
	imported := 0
	deleted := 0
	if result != nil {
		imported = result.Imported
		deleted = result.Deleted
	}
	slog.Info("stream source run finished", "kind", kind, "source_id", source.ID, "run_id", runID, "imported", imported, "deleted", deleted)
	return nil
}

func (r *Runner) applyResult(module Module, source *StreamSource, result *RunResult) error {
	if result == nil {
		slog.Info("stream source returned no result", "kind", module.Kind())
		return nil
	}
	slog.Info("applying stream source result", "kind", module.Kind(), "items", len(result.Items), "prune_missing", result.PruneMissing, "prune_ids", len(result.PruneSourceIDs))

	for _, item := range result.Items {
		record, err := item.ToRecord()
		if err != nil {
			return err
		}
		evaluation, err := sources.Reevaluate(module.EventType(), record.Url, record.Data, source.Settings())
		if err != nil {
			return err
		}

		event := &Event{
			Title:           record.Title,
			SourceId:        record.SourceId,
			Timestamp:       record.Timestamp,
			Type:            module.EventType(),
			Url:             record.Url,
			Data:            record.Data,
			SummaryRendered: evaluation.SummaryRendered,
			Hidden:          evaluation.Hidden,
		}
		if err := r.events.Upsert(event); err != nil {
			return err
		}
		result.Imported++
	}

	if result.PruneMissing && len(result.PruneSourceIDs) > 0 {
		rows, err := r.events.DeleteMissingByType(module.EventType(), result.PruneSourceIDs)
		if err != nil {
			return err
		}
		result.Deleted = int(rows)
	}

	if len(result.SettingsUpdates) > 0 {
		settings := source.Settings()
		for key, value := range result.SettingsUpdates {
			settings[key] = value
		}
		if err := source.SetSettings(settings); err != nil {
			return err
		}
		if err := r.sources.Save(source); err != nil {
			return err
		}
	}
	slog.Info("applied stream source result", "kind", module.Kind(), "imported", result.Imported, "deleted", result.Deleted)

	return nil
}
