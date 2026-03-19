package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type BlueskyModule struct {
	client *http.Client
}

func NewBlueskyModule() *BlueskyModule {
	return &BlueskyModule{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (m *BlueskyModule) Kind() string { return "bluesky" }
func (m *BlueskyModule) Name() string { return "Bluesky" }
func (m *BlueskyModule) Description() string {
	return "Imports an author's Bluesky posts and reposts using the documented author-feed HTTP API."
}
func (m *BlueskyModule) EventType() string { return "bluesky" }

func (m *BlueskyModule) Fields() []SettingField {
	return []SettingField{
		{Name: "actor", Label: "Actor", Type: "text", Placeholder: "handle or did", Help: "Required. Example: jmoiron.bsky.social"},
		{Name: "appview_url", Label: "AppView URL", Type: "text", Placeholder: "https://public.api.bsky.app", Help: "Public author-feed endpoint. No auth required for public posts."},
		{Name: "incremental_pages", Label: "Incremental Pages", Type: "text", Placeholder: "3", Help: "How many 100-item pages scheduled and normal runs should scan."},
		{Name: "full_pages", Label: "Full Import Pages", Type: "text", Placeholder: "12", Help: "How many 100-item pages a full import should scan."},
		{Name: "identifier", Label: "Identifier", Type: "text", Placeholder: "optional handle or email", Help: "Optional. Only needed if you want authenticated requests later."},
		{Name: "app_password", Label: "App Password", Type: "password", Help: "Optional. Used only when identifier is also set."},
		{Name: "pds_url", Label: "PDS URL", Type: "text", Placeholder: "https://bsky.social", Help: "Optional auth endpoint for createSession when using app password."},
	}
}

func (m *BlueskyModule) DefaultSettings() map[string]string {
	return map[string]string{
		"actor":             "",
		"appview_url":       "https://public.api.bsky.app",
		"incremental_pages": "3",
		"full_pages":        "12",
		"identifier":        "",
		"app_password":      "",
		"pds_url":           "https://bsky.social",
	}
}

func (m *BlueskyModule) DefaultScheduleMinutes() int { return 30 }

func (m *BlueskyModule) Sync(ctx context.Context, source *StreamSource, events *EventService) (*RunResult, error) {
	settings := source.Settings()
	actor := strings.TrimSpace(settings["actor"])
	appviewURL := strings.TrimRight(strings.TrimSpace(settings["appview_url"]), "/")
	identifier := strings.TrimSpace(settings["identifier"])
	password := strings.TrimSpace(settings["app_password"])
	pdsURL := strings.TrimRight(strings.TrimSpace(settings["pds_url"]), "/")

	if actor == "" {
		return nil, fmt.Errorf("bluesky actor is required")
	}
	if appviewURL == "" {
		appviewURL = "https://public.api.bsky.app"
	}

	token := ""
	feedBaseURL := appviewURL

	// Public author-feed reads do not require authentication. If credentials
	// are supplied, fall back to an authenticated request path via the user's PDS.
	if identifier != "" && password != "" {
		if pdsURL == "" {
			pdsURL = "https://bsky.social"
		}
		session, err := m.createSession(ctx, pdsURL, identifier, password)
		if err != nil {
			return nil, err
		}
		token = session.AccessJWT
		feedBaseURL = pdsURL
	}

	imported := 0
	lastSuccess := source.LastSuccessTime()
	mode := syncModeFromContext(ctx)
	maxPages := parsePositiveInt(settings["incremental_pages"], 3)
	if mode == SyncModeFull {
		maxPages = parsePositiveInt(settings["full_pages"], 12)
	}
	cursor := ""
	for page := 0; page < maxPages; page++ {
		feed, nextCursor, err := m.fetchAuthorFeed(ctx, feedBaseURL, token, actor, cursor)
		if err != nil {
			return nil, err
		}
		if len(feed) == 0 {
			break
		}

		stop := false
		for _, item := range feed {
			event, err := item.toEvent()
			if err != nil {
				return nil, err
			}
			if mode != SyncModeFull && !lastSuccess.IsZero() && event.Timestamp.Before(lastSuccess.Add(-24*time.Hour)) {
				stop = true
			}
			if err := events.Upsert(event); err != nil {
				return nil, err
			}
			imported++
		}
		if stop || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	return &RunResult{
		Imported: imported,
		Details: map[string]any{
			"actor":       actor,
			"auth_used":   token != "",
			"appview_url": appviewURL,
			"sync_mode":   string(mode),
			"pages":       maxPages,
		},
	}, nil
}

type blueskySession struct {
	AccessJWT string `json:"accessJwt"`
}

func (m *BlueskyModule) createSession(ctx context.Context, serviceURL, identifier, password string) (*blueskySession, error) {
	body, _ := json.Marshal(map[string]string{
		"identifier": identifier,
		"password":   password,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serviceURL+"/xrpc/com.atproto.server.createSession", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("bluesky session request returned %s", res.Status)
	}

	var session blueskySession
	if err := json.NewDecoder(res.Body).Decode(&session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (m *BlueskyModule) fetchAuthorFeed(ctx context.Context, serviceURL, token, actor, cursor string) ([]blueskyFeedItem, string, error) {
	values := url.Values{}
	values.Set("actor", actor)
	values.Set("limit", "100")
	if cursor != "" {
		values.Set("cursor", cursor)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL+"/xrpc/app.bsky.feed.getAuthorFeed?"+values.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := m.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		return nil, "", fmt.Errorf("bluesky feed request returned %s", res.Status)
	}

	var payload struct {
		Feed   []blueskyFeedItem `json:"feed"`
		Cursor string            `json:"cursor"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, "", err
	}
	return payload.Feed, payload.Cursor, nil
}

type blueskyFeedItem struct {
	Post struct {
		URI    string          `json:"uri"`
		Author blueskyAuthor   `json:"author"`
		Record json.RawMessage `json:"record"`
	} `json:"post"`
	Reply *struct {
		Parent map[string]any `json:"parent"`
	} `json:"reply"`
	Reason *struct {
		Type      string        `json:"$type"`
		By        blueskyAuthor `json:"by"`
		IndexedAt time.Time     `json:"indexedAt"`
	} `json:"reason"`
}

type blueskyAuthor struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
}

type blueskyRecord struct {
	Type      string `json:"$type"`
	Text      string `json:"text"`
	CreatedAt string `json:"createdAt"`
}

func (i blueskyFeedItem) toEvent() (*Event, error) {
	var record blueskyRecord
	if err := json.Unmarshal(i.Post.Record, &record); err != nil {
		return nil, err
	}

	ts, err := time.Parse(time.RFC3339, record.CreatedAt)
	if err != nil {
		ts = time.Now()
	}

	postURL := blueskyPostURL(i.Post.Author.Handle, i.Post.URI)
	sourceID := i.Post.URI
	text := truncateText(record.Text, 280)
	title := "post"

	if i.Reason != nil && i.Reason.Type == "app.bsky.feed.defs#reasonRepost" {
		sourceID = "repost:" + i.Post.URI
		ts = i.Reason.IndexedAt
		title = "repost"
		text = "reposted: " + text
	}
	if i.Reply != nil {
		title = "reply"
		text = "replied: " + text
	}

	raw, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}

	actor := i.Post.Author.Handle
	if i.Post.Author.DisplayName != "" {
		actor = i.Post.Author.DisplayName
	}

	return &Event{
		Title:           title,
		SourceId:        sourceID,
		Timestamp:       ts,
		Type:            "bluesky",
		Url:             postURL,
		Data:            string(raw),
		SummaryRendered: renderBlueskySummary(postURL, actor, text),
	}, nil
}

func blueskyPostURL(handle, atURI string) string {
	parts := strings.Split(strings.TrimPrefix(atURI, "at://"), "/")
	if len(parts) < 3 {
		return "https://bsky.app/profile/" + handle
	}
	return fmt.Sprintf("https://bsky.app/profile/%s/post/%s", handle, parts[2])
}

func parsePositiveInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
