package stream

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/monet/stream/sources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testModule struct {
	result *RunResult
}

func (m testModule) Kind() string                       { return "test" }
func (m testModule) Name() string                       { return "Test" }
func (m testModule) Description() string                { return "test module" }
func (m testModule) EventType() string                  { return "github" }
func (m testModule) Fields() []SettingField             { return nil }
func (m testModule) DefaultSettings() map[string]string { return map[string]string{} }
func (m testModule) DefaultScheduleMinutes() int        { return 60 }
func (m testModule) Sync(context.Context, sources.SourceConfig) (*RunResult, error) {
	return m.result, nil
}

type testItem struct {
	record *sources.Record
}

func (i testItem) ToRecord() (*sources.Record, error) { return i.record, nil }

func TestRunnerApplyResultUpsertsEvents(t *testing.T) {
	assert := assert.New(t)
	db := newTestStreamDB(t)
	runner := NewRunner(db, NewModuleRegistry())

	result := &RunResult{
		Items: []sources.Item{
			testItem{record: &sources.Record{
				Title:           "commit",
				SourceId:        "1111111111111111111111111111111111111111",
				Timestamp:       time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC),
				Url:             "https://github.com/jmoiron/monet/commit/1111111111111111111111111111111111111111",
				Data:            `{"kind":"commit","repo":"jmoiron/monet","ref":"refs/heads/main","commit":{"sha":"1111111111111111111111111111111111111111","author":{"login":"jmoiron"},"commit":{"message":"hello"}},"commit_url":"https://github.com/jmoiron/monet/commit/1111111111111111111111111111111111111111"}`,
				SummaryRendered: "<div>stale</div>",
			}},
		},
	}

	err := runner.applyResult(testModule{result: result}, &StreamSource{}, result)
	require.NoError(t, err)
	assert.Equal(1, result.Imported)

	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM event WHERE type='github'`))
	assert.Equal(1, count)
}
