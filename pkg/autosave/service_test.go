package autosave

import (
	"strings"
	"testing"

	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Connect("sqlite3", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	manager, err := monarch.NewManager(db)
	require.NoError(t, err)
	require.NoError(t, manager.Upgrade(Migrations()))

	return db
}

func TestLoadWithDiffs(t *testing.T) {
	assert := assert.New(t)
	db := setupTestDB(t)
	service := NewService(db)

	savedContent := "line one\nline two\nline three\n"
	autosaveContent := "line one\nline two changed\nline three\n"

	require.NoError(t, service.Save("test", 1, autosaveContent, "title"))

	results, err := service.LoadWithDiffs("test", 1, savedContent)
	assert.NoError(err)
	assert.Len(results, 1)

	diff := results[0].Diff
	assert.NotEmpty(diff)
	// unified diff marks removed lines with - and added lines with +
	assert.True(strings.Contains(diff, "-line two\n"), "diff should mark removed line")
	assert.True(strings.Contains(diff, "+line two changed\n"), "diff should mark added line")
}

func TestLoadWithDiffsNoChange(t *testing.T) {
	assert := assert.New(t)
	db := setupTestDB(t)
	service := NewService(db)

	content := "same content\n"
	require.NoError(t, service.Save("test", 1, content, "title"))

	results, err := service.LoadWithDiffs("test", 1, content)
	assert.NoError(err)
	assert.Len(results, 1)
	assert.Empty(results[0].Diff, "identical content should produce empty diff")
}

func TestLoadWithDiffsOrdering(t *testing.T) {
	assert := assert.New(t)
	db := setupTestDB(t)
	service := NewService(db)

	require.NoError(t, service.Save("test", 1, "version one\n", "v1"))
	require.NoError(t, service.Save("test", 1, "version two\n", "v2"))

	results, err := service.LoadWithDiffs("test", 1, "current saved\n")
	assert.NoError(err)
	assert.Len(results, 2)
	// most recent first
	assert.Equal("v2", results[0].Title)
	assert.Equal("v1", results[1].Title)
	// both diffs should be non-empty since content differs from saved
	assert.NotEmpty(results[0].Diff)
	assert.NotEmpty(results[1].Diff)
}
