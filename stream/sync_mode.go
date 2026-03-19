package stream

import "context"

type syncModeKey struct{}

type SyncMode string

const (
	SyncModeIncremental SyncMode = "incremental"
	SyncModeFull        SyncMode = "full"
)

func WithSyncMode(ctx context.Context, mode SyncMode) context.Context {
	return context.WithValue(ctx, syncModeKey{}, mode)
}

func syncModeFromContext(ctx context.Context) SyncMode {
	if ctx == nil {
		return SyncModeIncremental
	}
	if mode, ok := ctx.Value(syncModeKey{}).(SyncMode); ok {
		return mode
	}
	return SyncModeIncremental
}
