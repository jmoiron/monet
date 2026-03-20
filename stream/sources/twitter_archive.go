package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type TwitterArchiveModule struct{}

func NewTwitterArchive() *TwitterArchiveModule { return &TwitterArchiveModule{} }

func (m *TwitterArchiveModule) Kind() string { return "twitter_archive" }
func (m *TwitterArchiveModule) Name() string { return "Twitter Archive" }
func (m *TwitterArchiveModule) Description() string {
	return "Imports tweets from a Twitter archive upload and reconciles them with older stored Twitter stream rows."
}
func (m *TwitterArchiveModule) EventType() string { return "twitter" }

func (m *TwitterArchiveModule) Fields() []SettingField {
	return []SettingField{
		{Name: "display_name", Label: "Display Name", Type: "text", Placeholder: "Jason Moiron"},
		{Name: "username", Label: "Username", Type: "text", Placeholder: "jmoiron"},
	}
}

func (m *TwitterArchiveModule) DefaultSettings() map[string]string {
	return map[string]string{
		"display_name": "Jason Moiron",
		"username":     "jmoiron",
	}
}

func (m *TwitterArchiveModule) DefaultScheduleMinutes() int { return 0 }

func (m *TwitterArchiveModule) Sync(_ context.Context, _ SourceConfig) (*RunResult, error) {
	return nil, fmt.Errorf("twitter archive imports are manual upload only")
}

func (m *TwitterArchiveModule) ImportArchive(filename string, buf []byte, source SourceConfig) (*RunResult, error) {
	settings := source.Settings()
	displayName := strings.TrimSpace(settings["display_name"])
	username := strings.TrimSpace(settings["username"])
	if displayName == "" {
		displayName = username
	}
	if username == "" {
		username = "jmoiron"
	}
	slog.Info("starting twitter archive import", "filename", filename, "username", username, "display_name", displayName)

	tweets, err := loadTwitterArchiveBytes(filename, buf)
	if err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(tweets))
	for _, item := range tweets {
		record, err := item.ToRecordWithProfile(displayName, username)
		if err != nil {
			return nil, err
		}
		if record.SourceId == "" {
			continue
		}
		items = append(items, twitterArchiveItem{record: record})
	}

	slog.Info("finished twitter archive import", "filename", filename, "items", len(items))

	return &RunResult{
		Items: items,
		Details: map[string]any{
			"filename": filename,
			"tweets":   len(items),
		},
	}, nil
}

type twitterArchiveEntry struct {
	Tweet twitterArchiveTweet `json:"tweet"`
	Raw   json.RawMessage     `json:"-"`
}

func (e twitterArchiveEntry) ToRecordWithProfile(displayName, username string) (*Record, error) {
	ts, err := time.Parse("Mon Jan 02 15:04:05 -0700 2006", e.Tweet.CreatedAt)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://twitter.com/%s/status/%s", username, e.Tweet.IDStr)
	body := truncateText(e.Tweet.body(), 280)
	archiveRaw := e.Raw
	if len(archiveRaw) == 0 {
		buf, err := json.Marshal(e)
		if err != nil {
			return nil, err
		}
		archiveRaw = buf
	}
	raw, err := json.Marshal(map[string]any{
		"profile": map[string]any{
			"display_name": displayName,
			"username":     username,
		},
		"archive": json.RawMessage(archiveRaw),
	})
	if err != nil {
		return nil, err
	}
	return &Record{
		Title:           "tweet",
		SourceId:        e.Tweet.IDStr,
		Timestamp:       ts,
		Url:             url,
		Data:            string(raw),
		SummaryRendered: renderTwitterSummary(url, body),
	}, nil
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

func loadTwitterArchiveBytes(filename string, buf []byte) ([]twitterArchiveEntry, error) {
	slog.Info("loading twitter archive data", "filename", filename, "bytes", len(buf))
	raw := string(buf)
	start := strings.Index(raw, "[")
	if start < 0 {
		return nil, fmt.Errorf("twitter archive %s did not contain json array", filename)
	}

	var rawEntries []json.RawMessage
	if err := json.Unmarshal([]byte(raw[start:]), &rawEntries); err != nil {
		return nil, err
	}

	entries := make([]twitterArchiveEntry, 0, len(rawEntries))
	for _, rawEntry := range rawEntries {
		var entry twitterArchiveEntry
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			return nil, err
		}
		entry.Raw = append([]byte(nil), rawEntry...)
		entries = append(entries, entry)
	}

	slog.Info("loaded twitter archive data", "filename", filename, "entries", len(entries))
	return entries, nil
}
