package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/stream/sources"
)

type Admin struct {
	db      db.DB
	sources *SourceService
	events  *EventService
	runner  *Runner
	modules *ModuleRegistry
}

func NewAdmin(database db.DB, runner *Runner, modules *ModuleRegistry) *Admin {
	return &Admin{
		db:      database,
		sources: NewSourceService(database),
		events:  NewEventService(database),
		runner:  runner,
		modules: modules,
	}
}

func (a *Admin) Bind(r chi.Router) {
	r.Get("/stream/", a.index)
	r.Get("/stream/source/{kind}", a.edit)
	r.Post("/stream/source/{kind}", a.save)
	r.Post("/stream/source/{kind}/import", a.importArchive)
	r.Post("/stream/run/{kind}", a.runNow)
	r.Post("/stream/run/{kind}/full", a.runFull)
	r.Post("/stream/run/{kind}/repo", a.runRepoBackfill)
	r.Post("/stream/run/{kind}/rerender", a.rerender)
}

func (a *Admin) Panels(r *http.Request) ([]string, error) {
	sources, err := a.sources.List()
	if err != nil {
		return nil, err
	}

	var items []map[string]any
	for _, source := range sources {
		module, ok := a.modules.Get(source.Kind)
		if !ok {
			continue
		}
		count, _ := a.events.CountByType(module.EventType())
		items = append(items, map[string]any{
			"name":        source.Name,
			"kind":        source.Kind,
			"enabled":     source.Enabled,
			"count":       count,
			"lastRunAt":   source.LastRunAt,
			"isRunning":   a.runner.IsRunning(source.Kind),
			"schedule":    source.ScheduleMinutes,
			"lastError":   source.LastError,
			"description": module.Description(),
		})
	}

	var b bytes.Buffer
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.Render(&b, "stream/admin/panel.html", mtr.Ctx{
		"title":   "Stream",
		"fullUrl": "/admin/stream/",
		"sources": items,
	}); err != nil {
		return nil, err
	}

	return []string{b.String()}, nil
}

func (a *Admin) index(w http.ResponseWriter, r *http.Request) {
	sources, err := a.sources.List()
	if err != nil {
		app.Http500("loading stream sources", w, err)
		return
	}

	var items []map[string]any
	for _, source := range sources {
		module, ok := a.modules.Get(source.Kind)
		if !ok {
			continue
		}
		count, _ := a.events.CountByType(module.EventType())
		items = append(items, map[string]any{
			"source":      source,
			"module":      module,
			"eventCount":  count,
			"isRunning":   a.runner.IsRunning(source.Kind),
			"lastError":   source.LastError,
			"lastRunAt":   source.LastRunAt,
			"lastSuccess": source.LastSuccessAt,
			"lastSuccessFmt": func() string {
				if source.LastSuccessAt == 0 {
					return "never"
				}
				return app.FmtTimestamp(source.LastSuccessAt)
			}(),
		})
	}

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "admin-base", "stream/admin/index.html", mtr.Ctx{
		"title":   "Stream",
		"sources": items,
	}); err != nil {
		app.Http500("rendering stream admin", w, err)
	}
}

func (a *Admin) edit(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	module, ok := a.modules.Get(kind)
	if !ok {
		app.Http404(w)
		return
	}

	source, err := a.sources.GetByKind(kind)
	if err != nil {
		app.Http500("loading stream source", w, err)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "admin-base", "stream/admin/source-edit.html", mtr.Ctx{
		"title":    fmt.Sprintf("Stream: %s", module.Name()),
		"source":   source,
		"module":   module,
		"fields":   module.Fields(),
		"settings": source.Settings(),
		"running":  a.runner.IsRunning(kind),
	}); err != nil {
		app.Http500("rendering stream source edit", w, err)
	}
}

func (a *Admin) save(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	module, ok := a.modules.Get(kind)
	if !ok {
		app.Http404(w)
		return
	}

	source, err := a.sources.GetByKind(kind)
	if err != nil {
		app.Http500("loading stream source", w, err)
		return
	}

	if err := r.ParseForm(); err != nil {
		app.Http500("parsing stream source form", w, err)
		return
	}

	source.Name = r.FormValue("name")
	if source.Kind == "twitter_archive" {
		source.Enabled = false
		source.ScheduleMinutes = 0
	} else {
		source.Enabled = r.FormValue("enabled") == "on"
		source.ScheduleMinutes, _ = strconv.Atoi(r.FormValue("schedule_minutes"))
		if source.ScheduleMinutes < 0 {
			source.ScheduleMinutes = 0
		}
	}

	settings := source.Settings()
	for _, field := range module.Fields() {
		switch field.Type {
		case "checkbox":
			if r.FormValue(field.Name) == "on" {
				settings[field.Name] = "true"
			} else {
				settings[field.Name] = "false"
			}
		default:
			settings[field.Name] = r.FormValue(field.Name)
		}
	}
	if err := source.SetSettings(settings); err != nil {
		app.Http500("saving stream source settings", w, err)
		return
	}
	if err := a.sources.Save(source); err != nil {
		app.Http500("saving stream source", w, err)
		return
	}

	http.Redirect(w, r, "/admin/stream/", http.StatusSeeOther)
}

func (a *Admin) importArchive(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	module, ok := a.modules.Get(kind)
	if !ok {
		app.Http404(w)
		return
	}

	twitterModule, ok := module.(*sources.TwitterArchiveModule)
	if !ok {
		app.Http404(w)
		return
	}

	source, err := a.sources.GetByKind(kind)
	if err != nil {
		app.Http500("loading stream source", w, err)
		return
	}

	if err := r.ParseMultipartForm(16 << 20); err != nil {
		app.Http500("parsing twitter archive upload", w, err)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		app.Http500("loading twitter archive upload", w, err)
		return
	}
	defer file.Close()

	buf, err := io.ReadAll(file)
	if err != nil {
		app.Http500("reading twitter archive upload", w, err)
		return
	}

	slog.Info("starting twitter archive admin import", "filename", header.Filename, "bytes", len(buf))
	if err := a.sources.MarkRunStart(source.ID); err != nil {
		app.Http500("marking twitter archive run start", w, err)
		return
	}

	runID, err := a.sources.CreateRun(source.ID)
	if err != nil {
		app.Http500("creating twitter archive run", w, err)
		return
	}

	result, runErr := twitterModule.ImportArchive(header.Filename, buf, source)
	if runErr == nil {
		mergedCount := 0
		for i, item := range result.Items {
			record, err := item.ToRecord()
			if err != nil {
				runErr = err
				break
			}
			existing, err := a.events.GetByTypeAndSourceID(module.EventType(), record.SourceId)
			if err == nil && existing != nil {
				record.Data, err = mergeTwitterArchiveData(existing.Data, record.Data)
				if err != nil {
					runErr = err
					break
				}
				result.Items[i] = sources.StaticItem{Record: record}
				mergedCount++
			}
		}
		if runErr == nil {
			if result.Details == nil {
				result.Details = map[string]any{}
			}
			result.Details["merged_existing"] = mergedCount
			runErr = a.runner.applyResult(module, source, result)
		}
	}

	if finishErr := a.sources.FinishRun(runID, result, runErr); finishErr != nil {
		slog.Error("finishing twitter archive import run", "err", finishErr)
	}
	if markErr := a.sources.MarkRunResult(source.ID, result, runErr); markErr != nil {
		slog.Error("marking twitter archive import result", "err", markErr)
	}
	if runErr != nil {
		app.Http500("importing twitter archive", w, runErr)
		return
	}

	http.Redirect(w, r, "/admin/stream/source/"+kind, http.StatusSeeOther)
}

func mergeTwitterArchiveData(existingData, archiveData string) (string, error) {
	if strings.TrimSpace(existingData) == "" {
		return archiveData, nil
	}

	var archiveEnvelope struct {
		Profile json.RawMessage `json:"profile"`
		Archive json.RawMessage `json:"archive"`
	}
	if err := json.Unmarshal([]byte(archiveData), &archiveEnvelope); err == nil && len(archiveEnvelope.Archive) > 0 {
		legacy := json.RawMessage(existingData)
		var existingCombined struct {
			Legacy  json.RawMessage `json:"legacy"`
			Archive json.RawMessage `json:"archive"`
		}
		if err := json.Unmarshal([]byte(existingData), &existingCombined); err == nil && len(existingCombined.Legacy) > 0 {
			legacy = existingCombined.Legacy
		}

		buf, err := json.Marshal(map[string]any{
			"profile": archiveEnvelope.Profile,
			"legacy":  legacy,
			"archive": archiveEnvelope.Archive,
		})
		if err != nil {
			return "", err
		}
		return string(buf), nil
	}

	legacy := json.RawMessage(existingData)
	var existingCombined struct {
		Legacy json.RawMessage `json:"legacy"`
	}
	if err := json.Unmarshal([]byte(existingData), &existingCombined); err == nil && len(existingCombined.Legacy) > 0 {
		legacy = existingCombined.Legacy
	}

	archive := json.RawMessage(archiveData)
	var merged struct {
		Legacy  json.RawMessage `json:"legacy"`
		Archive json.RawMessage `json:"archive"`
	}
	merged.Legacy = legacy
	merged.Archive = archive

	buf, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func (a *Admin) runNow(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	if err := a.runner.Run(r.Context(), kind); err != nil {
		app.Http500("running stream source", w, err)
		return
	}
	http.Redirect(w, r, "/admin/stream/", http.StatusSeeOther)
}

func (a *Admin) runFull(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	ctx := WithSyncMode(r.Context(), SyncModeFull)
	if err := a.runner.Run(ctx, kind); err != nil {
		app.Http500("running full stream import", w, err)
		return
	}
	http.Redirect(w, r, "/admin/stream/", http.StatusSeeOther)
}

func (a *Admin) runRepoBackfill(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	repo := strings.TrimSpace(r.FormValue("repo"))
	if repo == "" {
		app.Http500("running repo backfill", w, fmt.Errorf("invalid repo"))
		return
	}

	ctx := WithRepoBackfill(r.Context(), repo)
	if err := a.runner.Run(ctx, kind); err != nil {
		app.Http500("running repo backfill", w, err)
		return
	}
	http.Redirect(w, r, "/admin/stream/source/"+kind, http.StatusSeeOther)
}

func (a *Admin) rerender(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	module, ok := a.modules.Get(kind)
	if !ok {
		app.Http404(w)
		return
	}

	source, err := a.sources.GetByKind(kind)
	if err != nil {
		app.Http500("loading stream source", w, err)
		return
	}

	updated, err := a.events.RerenderByType(module.EventType(), source.Settings())
	if err != nil {
		app.Http500("rerendering stream source events", w, err)
		return
	}
	slog.Info("rerendered stream source events", "kind", kind, "event_type", module.EventType(), "updated", updated)
	http.Redirect(w, r, "/admin/stream/", http.StatusSeeOther)
}
