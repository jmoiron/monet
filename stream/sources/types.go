package sources

import (
	"context"
	"time"
)

type SettingField struct {
	Name        string
	Label       string
	Type        string
	Placeholder string
	Help        string
}

type RunResult struct {
	Items           []Item
	PruneMissing    bool
	PruneSourceIDs  []string
	Imported        int
	Deleted         int
	Details         map[string]any
	SettingsUpdates map[string]string
}

type Record struct {
	Title           string
	SourceId        string
	Timestamp       time.Time
	Url             string
	Data            string
	SummaryRendered string
}

type SourceConfig interface {
	Settings() map[string]string
	LastSuccessTime() time.Time
}

type Item interface {
	ToRecord() (*Record, error)
}

type StaticItem struct {
	Record *Record
}

func (i StaticItem) ToRecord() (*Record, error) {
	return i.Record, nil
}

type Module interface {
	Kind() string
	Name() string
	Description() string
	EventType() string
	Fields() []SettingField
	DefaultSettings() map[string]string
	DefaultScheduleMinutes() int
	Sync(context.Context, SourceConfig) (*RunResult, error)
}

type syncModeKey struct{}
type pageOverrideKey struct{}

type SyncMode string

const (
	SyncModeIncremental SyncMode = "incremental"
	SyncModeFull        SyncMode = "full"
)

func WithSyncMode(ctx context.Context, mode SyncMode) context.Context {
	return context.WithValue(ctx, syncModeKey{}, mode)
}

func SyncModeFromContext(ctx context.Context) SyncMode {
	if ctx == nil {
		return SyncModeIncremental
	}
	if mode, ok := ctx.Value(syncModeKey{}).(SyncMode); ok {
		return mode
	}
	return SyncModeIncremental
}

func WithPageOverride(ctx context.Context, page int) context.Context {
	return context.WithValue(ctx, pageOverrideKey{}, page)
}

func PageOverrideFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if page, ok := ctx.Value(pageOverrideKey{}).(int); ok && page > 0 {
		return page
	}
	return 0
}
