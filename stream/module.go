package stream

import (
	"context"
	"fmt"
	"sort"
)

type SettingField struct {
	Name        string
	Label       string
	Type        string
	Placeholder string
	Help        string
}

type RunResult struct {
	Imported int
	Deleted  int
	Details  map[string]any
}

type Module interface {
	Kind() string
	Name() string
	Description() string
	EventType() string
	Fields() []SettingField
	DefaultSettings() map[string]string
	DefaultScheduleMinutes() int
	Sync(context.Context, *StreamSource, *EventService) (*RunResult, error)
}

type ModuleRegistry struct {
	modules map[string]Module
}

func NewModuleRegistry(modules ...Module) *ModuleRegistry {
	registry := &ModuleRegistry{modules: map[string]Module{}}
	for _, module := range modules {
		registry.modules[module.Kind()] = module
	}
	return registry
}

func (r *ModuleRegistry) Get(kind string) (Module, bool) {
	module, ok := r.modules[kind]
	return module, ok
}

func (r *ModuleRegistry) MustGet(kind string) Module {
	module, ok := r.Get(kind)
	if !ok {
		panic(fmt.Sprintf("unknown stream module %q", kind))
	}
	return module
}

func (r *ModuleRegistry) List() []Module {
	modules := make([]Module, 0, len(r.modules))
	for _, module := range r.modules {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Name() < modules[j].Name()
	})
	return modules
}
