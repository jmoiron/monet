package stream

import (
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
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
	r.Post("/stream/run/{kind}", a.runNow)
	r.Post("/stream/run/{kind}/full", a.runFull)
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
	source.Enabled = r.FormValue("enabled") == "on"
	source.ScheduleMinutes, _ = strconv.Atoi(r.FormValue("schedule_minutes"))
	if source.ScheduleMinutes < 0 {
		source.ScheduleMinutes = 0
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
