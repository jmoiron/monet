package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type GitHubModule struct {
	client *http.Client
}

func NewGitHubModule() *GitHubModule {
	return &GitHubModule{
		client: &http.Client{Timeout: 20 * time.Second},
	}
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
	}
}

func (m *GitHubModule) DefaultSettings() map[string]string {
	return map[string]string{
		"username": "jmoiron",
		"token":    "",
	}
}

func (m *GitHubModule) DefaultScheduleMinutes() int { return 60 }

func (m *GitHubModule) Sync(ctx context.Context, source *StreamSource, events *EventService) (*RunResult, error) {
	settings := source.Settings()
	username := strings.TrimSpace(settings["username"])
	if username == "" {
		return nil, fmt.Errorf("github username is required")
	}

	token := strings.TrimSpace(settings["token"])
	lastSuccess := source.LastSuccessTime()
	imported := 0

	for page := 1; page <= 3; page++ {
		items, err := m.fetchPage(ctx, username, token, page)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}

		stop := false
		for _, item := range items {
			if !lastSuccess.IsZero() && item.CreatedAt.Before(lastSuccess.Add(-24*time.Hour)) {
				stop = true
			}

			event, err := item.toEvent()
			if err != nil {
				return nil, err
			}
			if err := events.Upsert(event); err != nil {
				return nil, err
			}
			imported++
		}
		if stop {
			break
		}
	}

	return &RunResult{
		Imported: imported,
		Details: map[string]any{
			"username": username,
		},
	}, nil
}

func (m *GitHubModule) fetchPage(ctx context.Context, username, token string, page int) ([]githubEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("https://api.github.com/users/%s/events/public?per_page=100&page=%d", username, page), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "monet-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("github api returned %s", res.Status)
	}

	var events []githubEvent
	if err := json.NewDecoder(res.Body).Decode(&events); err != nil {
		return nil, err
	}
	return events, nil
}

type githubEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Repo      githubRepo      `json:"repo"`
	Payload   json.RawMessage `json:"payload"`
	Raw       json.RawMessage `json:"-"`
}

type githubRepo struct {
	Name string `json:"name"`
}

func (e *githubEvent) UnmarshalJSON(data []byte) error {
	type alias githubEvent
	aux := alias{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = githubEvent(aux)
	e.Raw = append([]byte(nil), data...)
	return nil
}

func (e githubEvent) toEvent() (*Event, error) {
	summary, url, title := e.summary()
	return &Event{
		Title:           title,
		SourceId:        e.ID,
		Timestamp:       e.CreatedAt,
		Type:            "github",
		Url:             url,
		Data:            string(e.Raw),
		SummaryRendered: summary,
	}, nil
}

func (e githubEvent) summary() (summary string, url string, title string) {
	repoURL := "https://github.com/" + e.Repo.Name
	url = repoURL
	title = e.Type
	text := strings.TrimSpace(e.Type)

	switch e.Type {
	case "PushEvent":
		var payload struct {
			Ref     string `json:"ref"`
			Head    string `json:"head"`
			Commits []struct {
				Message string `json:"message"`
			} `json:"commits"`
		}
		if json.Unmarshal(e.Payload, &payload) == nil {
			if payload.Head != "" {
				url = repoURL + "/commit/" + payload.Head
			}
			ref := strings.TrimPrefix(payload.Ref, "refs/heads/")
			message := "pushed to " + ref
			if len(payload.Commits) > 0 && strings.TrimSpace(payload.Commits[0].Message) != "" {
				message = truncateText(payload.Commits[0].Message, 180)
			}
			text = message
			title = "push"
		}
	case "CreateEvent":
		var payload struct {
			RefType string `json:"ref_type"`
			Ref     string `json:"ref"`
		}
		if json.Unmarshal(e.Payload, &payload) == nil {
			text = fmt.Sprintf("created %s %s", payload.RefType, strings.TrimSpace(payload.Ref))
			title = "create"
		}
	case "PullRequestEvent":
		var payload struct {
			Action      string `json:"action"`
			PullRequest struct {
				HTMLURL string `json:"html_url"`
				Number  int    `json:"number"`
				Title   string `json:"title"`
			} `json:"pull_request"`
		}
		if json.Unmarshal(e.Payload, &payload) == nil {
			if payload.PullRequest.HTMLURL != "" {
				url = payload.PullRequest.HTMLURL
			}
			text = fmt.Sprintf("%s pull request #%d: %s", payload.Action, payload.PullRequest.Number, truncateText(payload.PullRequest.Title, 160))
			title = "pull request"
		}
	case "IssuesEvent":
		var payload struct {
			Action string `json:"action"`
			Issue  struct {
				HTMLURL string `json:"html_url"`
				Number  int    `json:"number"`
				Title   string `json:"title"`
			} `json:"issue"`
		}
		if json.Unmarshal(e.Payload, &payload) == nil {
			if payload.Issue.HTMLURL != "" {
				url = payload.Issue.HTMLURL
			}
			text = fmt.Sprintf("%s issue #%d: %s", payload.Action, payload.Issue.Number, truncateText(payload.Issue.Title, 160))
			title = "issue"
		}
	case "IssueCommentEvent":
		var payload struct {
			Action string `json:"action"`
			Issue  struct {
				HTMLURL string `json:"html_url"`
				Number  int    `json:"number"`
				Title   string `json:"title"`
			} `json:"issue"`
		}
		if json.Unmarshal(e.Payload, &payload) == nil {
			if payload.Issue.HTMLURL != "" {
				url = payload.Issue.HTMLURL
			}
			text = fmt.Sprintf("%s comment on issue #%d: %s", payload.Action, payload.Issue.Number, truncateText(payload.Issue.Title, 160))
			title = "issue comment"
		}
	case "ReleaseEvent":
		var payload struct {
			Action  string `json:"action"`
			Release struct {
				HTMLURL string `json:"html_url"`
				TagName string `json:"tag_name"`
				Name    string `json:"name"`
			} `json:"release"`
		}
		if json.Unmarshal(e.Payload, &payload) == nil {
			if payload.Release.HTMLURL != "" {
				url = payload.Release.HTMLURL
			}
			name := payload.Release.Name
			if name == "" {
				name = payload.Release.TagName
			}
			text = fmt.Sprintf("%s release %s", payload.Action, truncateText(name, 160))
			title = "release"
		}
	case "WatchEvent":
		text = "starred repository"
		title = "star"
	}

	return renderGithubSummary(url, e.Repo.Name, text), url, title
}
