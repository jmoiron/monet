package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

type TwitterArchiveModule struct{}

func NewTwitterArchive() *TwitterArchiveModule { return &TwitterArchiveModule{} }

func (m *TwitterArchiveModule) Kind() string { return "twitter_archive" }
func (m *TwitterArchiveModule) Name() string { return "Twitter Archive" }
func (m *TwitterArchiveModule) Description() string {
	return "Imports legacy tweets from a Twitter export and can prune old stream rows missing from the archive."
}
func (m *TwitterArchiveModule) EventType() string { return "twitter" }

func (m *TwitterArchiveModule) Fields() []SettingField {
	return []SettingField{
		{Name: "archive_path", Label: "Archive Path", Type: "text", Placeholder: "exports/twitter/data/tweets.js"},
		{Name: "redact_path", Label: "Redact Path", Type: "text", Placeholder: "exports/redact/.../export_twitter_1.json", Help: "Used for audit counts while reconciling old Twitter data."},
		{Name: "username", Label: "Username", Type: "text", Placeholder: "jmoiron"},
		{Name: "prune_missing", Label: "Prune Missing", Type: "checkbox", Help: "Delete existing twitter stream rows not present in the archive after a successful import."},
	}
}

func (m *TwitterArchiveModule) DefaultSettings() map[string]string {
	return map[string]string{
		"archive_path":  "exports/twitter/data/tweets.js",
		"redact_path":   "exports/redact/Jason Moiron/twitter/json/export_twitter_1.json",
		"username":      "jmoiron",
		"prune_missing": "true",
	}
}

func (m *TwitterArchiveModule) DefaultScheduleMinutes() int { return 0 }

func (m *TwitterArchiveModule) Sync(_ context.Context, source SourceConfig) (*RunResult, error) {
	settings := source.Settings()
	archivePath := strings.TrimSpace(settings["archive_path"])
	username := strings.TrimSpace(settings["username"])
	if archivePath == "" {
		return nil, fmt.Errorf("twitter archive path is required")
	}
	if username == "" {
		username = "jmoiron"
	}
	slog.Info("starting twitter archive sync", "archive_path", archivePath, "username", username, "prune_missing", settings["prune_missing"] == "true")

	tweets, err := loadTwitterArchive(archivePath)
	if err != nil {
		return nil, err
	}

	keepIDs := make([]string, 0, len(tweets))
	items := make([]Item, 0, len(tweets))
	for _, item := range tweets {
		record, err := item.Tweet.ToRecordWithUsername(username)
		if err != nil {
			return nil, err
		}
		if record.SourceId == "" {
			continue
		}
		items = append(items, twitterArchiveItem{record: record})
		keepIDs = append(keepIDs, record.SourceId)
	}

	details := map[string]any{"archive_path": archivePath}
	if redactPath := strings.TrimSpace(settings["redact_path"]); redactPath != "" {
		slog.Info("loading twitter redact stats", "path", redactPath)
		if stats, err := loadRedactStats(redactPath); err == nil {
			details["redact_entries"] = stats.Entries
			details["redact_likes"] = stats.Likes
			slog.Info("loaded twitter redact stats", "path", redactPath, "entries", stats.Entries, "likes", stats.Likes)
		} else {
			slog.Warn("failed to load twitter redact stats", "path", redactPath, "err", err)
		}
	}

	slog.Info("finished twitter archive sync", "archive_path", archivePath, "items", len(items), "keep_ids", len(keepIDs))

	return &RunResult{
		Items:          items,
		PruneMissing:   settings["prune_missing"] == "true" && len(items) > 0,
		PruneSourceIDs: keepIDs,
		Details:        details,
	}, nil
}

type twitterArchiveEntry struct {
	Tweet twitterArchiveTweet `json:"tweet"`
}

type twitterArchiveTweet struct {
	IDStr     string `json:"id_str"`
	CreatedAt string `json:"created_at"`
	FullText  string `json:"full_text"`
	Text      string `json:"text"`
}

func (t twitterArchiveTweet) body() string {
	if strings.TrimSpace(t.FullText) != "" {
		return t.FullText
	}
	return t.Text
}

type twitterArchiveItem struct {
	record *Record
}

func (i twitterArchiveItem) ToRecord() (*Record, error) {
	return i.record, nil
}

func (t twitterArchiveTweet) ToRecordWithUsername(username string) (*Record, error) {
	ts, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", t.CreatedAt)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://twitter.com/%s/status/%s", username, t.IDStr)
	body := truncateText(t.body(), 280)
	raw, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return &Record{
		Title:           "tweet",
		SourceId:        t.IDStr,
		Timestamp:       ts,
		Url:             url,
		Data:            string(raw),
		SummaryRendered: renderTwitterSummary(url, body),
	}, nil
}

func loadTwitterArchive(path string) ([]twitterArchiveEntry, error) {
	slog.Info("loading twitter archive file", "path", path)
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	raw := string(buf)
	start := strings.Index(raw, "[")
	if start < 0 {
		return nil, fmt.Errorf("twitter archive %s did not contain json array", path)
	}

	var entries []twitterArchiveEntry
	if err := json.Unmarshal([]byte(raw[start:]), &entries); err != nil {
		return nil, err
	}
	slog.Info("loaded twitter archive file", "path", path, "entries", len(entries))
	return entries, nil
}

type redactStats struct {
	Entries int
	Likes   int
}

func loadRedactStats(path string) (*redactStats, error) {
	slog.Info("loading twitter redact file", "path", path)
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []struct {
		Thing struct {
			Display struct {
				Type string `json:"type"`
			} `json:"display"`
		} `json:"thing"`
	}
	if err := json.Unmarshal(buf, &entries); err != nil {
		return nil, err
	}

	stats := &redactStats{Entries: len(entries)}
	for _, entry := range entries {
		if entry.Thing.Display.Type == "LIKE" {
			stats.Likes++
		}
	}
	slog.Info("loaded twitter redact file", "path", path, "entries", stats.Entries, "likes", stats.Likes)
	return stats, nil
}
