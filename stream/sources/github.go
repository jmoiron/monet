package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type GitHubModule struct {
	client *http.Client
}

func NewGitHub() *GitHubModule {
	return &GitHubModule{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (m *GitHubModule) WithClient(client *http.Client) *GitHubModule {
	if client != nil {
		m.client = client
	}
	return m
}

func (m *GitHubModule) Kind() string { return "github" }
func (m *GitHubModule) Name() string { return "GitHub" }
func (m *GitHubModule) Description() string {
	return "Imports recent public GitHub activity for a user account."
}
func (m *GitHubModule) EventType() string { return "github" }

func (m *GitHubModule) Fields() []SettingField {
	return []SettingField{
		{Name: "username", Label: "Username", Type: "text", Placeholder: "jmoiron"},
		{Name: "token", Label: "Token", Type: "password", Help: "Optional personal access token to avoid rate limits."},
		{Name: "use_etag", Label: "Conditional Requests", Type: "checkbox", Help: "Use ETag/If-None-Match on the events feed. With an authorized request, GitHub says 304 responses do not count against the primary rate limit."},
	}
}

func (m *GitHubModule) DefaultSettings() map[string]string {
	return map[string]string{
		"username": "jmoiron",
		"token":    "",
		"use_etag": "true",
	}
}

func (m *GitHubModule) DefaultScheduleMinutes() int { return 60 }

func (m *GitHubModule) Sync(ctx context.Context, source SourceConfig) (*RunResult, error) {
	settings := source.Settings()
	username := strings.TrimSpace(settings["username"])
	if username == "" {
		return nil, fmt.Errorf("github username is required")
	}

	token := strings.TrimSpace(settings["token"])
	useETag := settings["use_etag"] == "true"
	eventsETag := strings.TrimSpace(settings["events_etag"])
	lastSuccess := source.LastSuccessTime()
	mode := SyncModeFromContext(ctx)
	results := make([]Item, 0, 300)
	runResult := &RunResult{
		Details: map[string]any{
			"username":  username,
			"sync_mode": string(mode),
		},
	}
	slog.Info("starting github sync", "username", username, "has_token", token != "", "use_etag", useETag, "has_events_etag", eventsETag != "", "sync_mode", mode, "last_success", lastSuccess)

	for page := 1; page <= 3; page++ {
		ifNoneMatch := ""
		if mode != SyncModeFull && useETag && page == 1 && eventsETag != "" {
			ifNoneMatch = eventsETag
		}
		pageResult, err := m.fetchPage(ctx, username, token, page, ifNoneMatch)
		if err != nil {
			return nil, err
		}
		if mode != SyncModeFull && page == 1 && pageResult.NotModified {
			slog.Info("github events feed not modified", "username", username, "page", page)
			runResult.Details["not_modified"] = true
			return runResult, nil
		}
		if page == 1 && useETag && pageResult.ETag != "" && pageResult.ETag != eventsETag {
			runResult.SettingsUpdates = map[string]string{
				"events_etag": pageResult.ETag,
			}
		}
		pageEvents := pageResult.Events
		if len(pageEvents) == 0 {
			break
		}

		stop := false
		for _, apiEvent := range pageEvents {
			items, err := m.expandEvent(ctx, token, username, apiEvent)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				record, err := item.ToRecord()
				if err != nil {
					return nil, err
				}
				if !lastSuccess.IsZero() && record.Timestamp.Before(lastSuccess.Add(-24*time.Hour)) {
					stop = true
				}
				results = append(results, item)
			}
		}
		if stop {
			break
		}
	}

	slog.Info("finished github sync", "username", username, "items", len(results))

	runResult.Items = results
	return runResult, nil
}

type githubPageResult struct {
	Events      []githubAPIEvent
	ETag        string
	NotModified bool
}

func (m *GitHubModule) fetchPage(ctx context.Context, username, token string, page int, ifNoneMatch string) (*githubPageResult, error) {
	reqURL := fmt.Sprintf("https://api.github.com/users/%s/events/public?per_page=100&page=%d", username, page)
	slog.Info("loading github url", "url", reqURL)
	req, err := m.newRequest(ctx, reqURL, token, ifNoneMatch)
	if err != nil {
		return nil, err
	}

	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified {
		slog.Info("github url not modified", "url", reqURL)
		return &githubPageResult{
			ETag:        strings.TrimSpace(res.Header.Get("ETag")),
			NotModified: true,
		}, nil
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("github api returned %s", res.Status)
	}

	var events []githubAPIEvent
	if err := json.NewDecoder(res.Body).Decode(&events); err != nil {
		return nil, err
	}
	etag := strings.TrimSpace(res.Header.Get("ETag"))
	slog.Info("loaded github events page", "page", page, "count", len(events), "has_etag", etag != "")
	return &githubPageResult{
		Events: events,
		ETag:   etag,
	}, nil
}

func (m *GitHubModule) expandEvent(ctx context.Context, token, username string, e githubAPIEvent) ([]Item, error) {
	slog.Debug("processing github event", "event_id", e.ID, "event_type", e.Type, "repo", e.Repo.Name)
	switch e.Type {
	case "PushEvent":
		return m.expandPushEvent(ctx, token, username, e)
	case "PullRequestEvent":
		item, err := m.expandPullRequestEvent(ctx, token, e)
		if err != nil {
			return nil, err
		}
		return []Item{item}, nil
	case "IssueCommentEvent":
		return []Item{githubIssueCommentItem{Event: e}}, nil
	case "PullRequestReviewCommentEvent":
		return []Item{githubPullRequestReviewCommentItem{Event: e}}, nil
	case "PullRequestReviewEvent":
		return []Item{githubPullRequestReviewItem{Event: e}}, nil
	default:
		return nil, nil
	}
}

func (m *GitHubModule) expandPullRequestEvent(ctx context.Context, token string, e githubAPIEvent) (Item, error) {
	var payload struct {
		PullRequest struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return githubPullRequestItem{Event: e}, nil
	}
	if strings.TrimSpace(payload.PullRequest.URL) == "" {
		return githubPullRequestItem{Event: e}, nil
	}

	prRaw, err := m.fetchPullRequest(ctx, token, payload.PullRequest.URL)
	if err != nil {
		return nil, err
	}
	return githubPullRequestItem{Event: e, PullRequestRaw: prRaw}, nil
}

func (m *GitHubModule) expandPushEvent(ctx context.Context, token, username string, e githubAPIEvent) ([]Item, error) {
	payload, err := e.pushPayload()
	if err != nil {
		return nil, err
	}

	commits := payload.Commits
	needCompare := len(commits) == 0 || (payload.Size > 0 && len(commits) < payload.Size) || !githubCommitsHaveAuthorLogins(commits)
	slog.Info("expanding github push event", "event_id", e.ID, "repo", e.Repo.Name, "ref", payload.Ref, "payload_commits", len(payload.Commits), "payload_size", payload.Size, "use_compare", needCompare, "username", username)
	if needCompare && payload.Before != "" && payload.Head != "" {
		compareCommits, err := m.fetchCompareCommits(ctx, token, e.Repo.Name, payload.Before, payload.Head)
		if err != nil {
			return nil, err
		}
		if len(compareCommits) > 0 {
			commits = compareCommits
		}
	}

	items := make([]Item, 0, len(commits))
	for _, commit := range commits {
		if strings.TrimSpace(commit.SHA) == "" {
			continue
		}
		if !githubCommitMatchesUsername(commit, username) {
			slog.Info("skipping github commit from push", "event_id", e.ID, "repo", e.Repo.Name, "sha", commit.SHA, "commit_author_login", strings.TrimSpace(commit.Author.Login), "username", username)
			continue
		}
		items = append(items, githubCommitItem{
			Event:  e,
			Commit: commit,
			Ref:    payload.Ref,
		})
	}
	return items, nil
}

func githubCommitsHaveAuthorLogins(commits []githubCommit) bool {
	for _, commit := range commits {
		if strings.TrimSpace(commit.Author.Login) == "" {
			return false
		}
	}
	return true
}

func githubCommitMatchesUsername(commit githubCommit, username string) bool {
	return strings.EqualFold(strings.TrimSpace(commit.Author.Login), strings.TrimSpace(username))
}

func (m *GitHubModule) fetchCompareCommits(ctx context.Context, token, repo, before, head string) ([]githubCommit, error) {
	reqURL := fmt.Sprintf("https://api.github.com/repos/%s/compare/%s...%s", repo, before, head)
	slog.Info("loading github url", "url", reqURL)
	req, err := m.newRequest(ctx, reqURL, token, "")
	if err != nil {
		return nil, err
	}

	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("github compare api returned %s", res.Status)
	}

	var payload struct {
		Commits []githubCommit `json:"commits"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	slog.Info("loaded github compare commits", "repo", repo, "before", before, "head", head, "count", len(payload.Commits))
	return payload.Commits, nil
}

func (m *GitHubModule) fetchPullRequest(ctx context.Context, token, apiURL string) (json.RawMessage, error) {
	slog.Info("loading github url", "url", apiURL)
	req, err := m.newRequest(ctx, apiURL, token, "")
	if err != nil {
		return nil, err
	}

	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("github pull request api returned %s", res.Status)
	}

	var payload json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	slog.Info("loaded github pull request", "url", apiURL)
	return payload, nil
}

func (m *GitHubModule) newRequest(ctx context.Context, url, token, ifNoneMatch string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "monet-stream")
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

type githubAPIEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Actor     struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"actor"`
	Repo    githubRepo      `json:"repo"`
	Payload json.RawMessage `json:"payload"`
	Raw     json.RawMessage `json:"-"`
}

type githubRepo struct {
	Name string `json:"name"`
}

type githubPushPayload struct {
	Ref     string         `json:"ref"`
	Head    string         `json:"head"`
	Before  string         `json:"before"`
	Size    int            `json:"size"`
	Commits []githubCommit `json:"commits"`
}

type githubCommit struct {
	SHA     string `json:"sha"`
	URL     string `json:"url"`
	HTMLURL string `json:"html_url"`
	Commit  struct {
		Message string `json:"message"`
		Author  struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
	} `json:"commit"`
	Author struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"author"`
}

func (e *githubAPIEvent) UnmarshalJSON(data []byte) error {
	type alias githubAPIEvent
	aux := alias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = githubAPIEvent(aux)
	e.Raw = append([]byte(nil), data...)
	return nil
}

func (e githubAPIEvent) pushPayload() (githubPushPayload, error) {
	var payload githubPushPayload
	err := json.Unmarshal(e.Payload, &payload)
	return payload, err
}

type githubCommitItem struct {
	Event  githubAPIEvent
	Commit githubCommit
	Ref    string
}

func (i githubCommitItem) ToRecord() (*Record, error) {
	repoURL := "https://github.com/" + i.Event.Repo.Name
	url := i.Commit.HTMLURL
	if url == "" {
		url = repoURL + "/commit/" + i.Commit.SHA
	}

	branch := strings.TrimPrefix(i.Ref, "refs/heads/")
	message := firstLine(strings.TrimSpace(i.Commit.Commit.Message))
	if message == "" {
		message = "commit " + shortSHA(i.Commit.SHA)
	}

	raw, err := json.Marshal(map[string]any{
		"kind":       "commit",
		"repo":       i.Event.Repo.Name,
		"ref":        i.Ref,
		"pushed_at":  i.Event.CreatedAt,
		"source":     json.RawMessage(i.Event.Raw),
		"commit":     i.Commit,
		"commit_url": url,
	})
	if err != nil {
		return nil, err
	}

	return &Record{
		Title:           "commit",
		SourceId:        i.Commit.SHA,
		Timestamp:       i.Event.CreatedAt,
		Url:             url,
		Data:            string(raw),
		SummaryRendered: renderGithubCommitSummary(url, i.Event.Repo.Name, branch, i.Commit.SHA, message),
	}, nil
}

func githubCommitSummaryText(branch, message string) string {
	if branch == "" {
		return truncateText(message, 180)
	}
	return truncateText(message+" ("+branch+")", 180)
}

func githubPullRequestSummaryRendered(url, repo string, number int, head, base, action string, merged bool) string {
	branchText := ""
	if head = strings.TrimSpace(head); head != "" {
		if base = strings.TrimSpace(base); base != "" {
			branchText = head + " → " + base
		}
	}
	return renderGithubPRSummary(url, repo, url, fmt.Sprintf("PR #%d", number), branchText, action)
}

type githubPullRequestItem struct {
	Event          githubAPIEvent
	PullRequestRaw json.RawMessage
}

func (i githubPullRequestItem) ToRecord() (*Record, error) {
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			URL     string `json:"url"`
			HTMLURL string `json:"html_url"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
			Body    string `json:"body"`
			Merged  bool   `json:"merged"`
			State   string `json:"state"`
			Head    struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(i.Event.Payload, &payload); err != nil {
		return nil, err
	}

	url := payload.PullRequest.HTMLURL
	if url == "" {
		url = "https://github.com/" + i.Event.Repo.Name
	}

	rawData := i.Event.Raw
	if len(i.PullRequestRaw) > 0 {
		enriched, err := json.Marshal(map[string]any{
			"kind":         "pull_request",
			"event":        json.RawMessage(i.Event.Raw),
			"pull_request": json.RawMessage(i.PullRequestRaw),
		})
		if err != nil {
			return nil, err
		}
		rawData = enriched
	}

	return &Record{
		Title:           "pull request",
		SourceId:        i.Event.ID,
		Timestamp:       i.Event.CreatedAt,
		Url:             url,
		Data:            string(rawData),
		SummaryRendered: githubPullRequestSummaryRendered(url, i.Event.Repo.Name, payload.PullRequest.Number, payload.PullRequest.Head.Ref, payload.PullRequest.Base.Ref, payload.Action, payload.PullRequest.Merged),
	}, nil
}

type githubIssueCommentItem struct {
	Event githubAPIEvent
}

func (i githubIssueCommentItem) ToRecord() (*Record, error) {
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			HTMLURL string `json:"html_url"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
		} `json:"issue"`
		Comment struct {
			HTMLURL string `json:"html_url"`
			Body    string `json:"body"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(i.Event.Payload, &payload); err != nil {
		return nil, err
	}

	url := payload.Comment.HTMLURL
	if url == "" {
		url = payload.Issue.HTMLURL
	}
	if url == "" {
		url = "https://github.com/" + i.Event.Repo.Name
	}

	snippet := firstLine(strings.TrimSpace(payload.Comment.Body))
	text := fmt.Sprintf("%s comment on issue #%d: %s", payload.Action, payload.Issue.Number, truncateText(payload.Issue.Title, 120))
	if snippet != "" {
		text += " - " + truncateText(snippet, 100)
	}

	return &Record{
		Title:           "issue comment",
		SourceId:        i.Event.ID,
		Timestamp:       i.Event.CreatedAt,
		Url:             url,
		Data:            string(i.Event.Raw),
		SummaryRendered: renderGithubSummary(url, i.Event.Repo.Name, text),
	}, nil
}

type githubPullRequestReviewCommentItem struct {
	Event githubAPIEvent
}

func (i githubPullRequestReviewCommentItem) ToRecord() (*Record, error) {
	var payload struct {
		Action      string `json:"action"`
		PullRequest struct {
			HTMLURL string `json:"html_url"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
		} `json:"pull_request"`
		Comment struct {
			HTMLURL string `json:"html_url"`
			Body    string `json:"body"`
		} `json:"comment"`
	}
	if err := json.Unmarshal(i.Event.Payload, &payload); err != nil {
		return nil, err
	}

	url := payload.Comment.HTMLURL
	if url == "" {
		url = payload.PullRequest.HTMLURL
	}
	if url == "" {
		url = "https://github.com/" + i.Event.Repo.Name
	}

	snippet := firstLine(strings.TrimSpace(payload.Comment.Body))
	text := fmt.Sprintf("%s review comment on PR #%d: %s", payload.Action, payload.PullRequest.Number, truncateText(payload.PullRequest.Title, 120))
	if snippet != "" {
		text += " - " + truncateText(snippet, 100)
	}

	return &Record{
		Title:           "review comment",
		SourceId:        i.Event.ID,
		Timestamp:       i.Event.CreatedAt,
		Url:             url,
		Data:            string(i.Event.Raw),
		SummaryRendered: renderGithubSummary(url, i.Event.Repo.Name, text),
	}, nil
}

type githubPullRequestReviewItem struct {
	Event githubAPIEvent
}

func (i githubPullRequestReviewItem) ToRecord() (*Record, error) {
	var payload struct {
		Action string `json:"action"`
		Review struct {
			HTMLURL string `json:"html_url"`
			Body    string `json:"body"`
			State   string `json:"state"`
		} `json:"review"`
		PullRequest struct {
			HTMLURL string `json:"html_url"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(i.Event.Payload, &payload); err != nil {
		return nil, err
	}

	url := payload.Review.HTMLURL
	if url == "" {
		url = payload.PullRequest.HTMLURL
	}
	if url == "" {
		url = "https://github.com/" + i.Event.Repo.Name
	}

	text := fmt.Sprintf("%s review on PR #%d: %s", payload.Action, payload.PullRequest.Number, truncateText(payload.PullRequest.Title, 120))
	if payload.Review.State != "" {
		text = fmt.Sprintf("%s review on PR #%d: %s", strings.ToLower(payload.Review.State), payload.PullRequest.Number, truncateText(payload.PullRequest.Title, 120))
	}
	snippet := firstLine(strings.TrimSpace(payload.Review.Body))
	if snippet != "" {
		text += " - " + truncateText(snippet, 100)
	}

	return &Record{
		Title:           "review",
		SourceId:        i.Event.ID,
		Timestamp:       i.Event.CreatedAt,
		Url:             url,
		Data:            string(i.Event.Raw),
		SummaryRendered: renderGithubSummary(url, i.Event.Repo.Name, text),
	}, nil
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}
