package sources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func RenderSummary(eventType, url, data string) (string, error) {
	switch eventType {
	case "github":
		return renderStoredGitHubSummary(url, data)
	case "bluesky":
		return renderStoredBlueskySummary(data)
	case "twitter":
		return renderStoredTwitterSummary(url, data)
	default:
		return "", fmt.Errorf("unsupported event type %q", eventType)
	}
}

func RenderDetail(eventType, title, url, data, summaryRendered string) (string, map[string]any, error) {
	switch eventType {
	case "bluesky":
		ctx, err := renderStoredBlueskyDetail(url, data)
		return "stream/detail/bluesky.html", ctx, err
	default:
		return "stream/detail/default.html", map[string]any{
			"title":            title,
			"url":              url,
			"summary_rendered": summaryRendered,
		}, nil
	}
}

func RenderGitHubSummary(url, repo, text string) string {
	return renderGithubSummary(url, repo, text)
}

func RenderBlueskySummary(url, actor, text string) string {
	return renderBlueskySummary(url, actor, text)
}

func RenderTwitterSummary(url, text string) string {
	return renderTwitterSummary(url, text)
}

func renderStoredTwitterSummary(url, data string) (string, error) {
	var tweet twitterArchiveTweet
	if err := json.Unmarshal([]byte(data), &tweet); err != nil {
		return "", err
	}
	return renderTwitterSummary(url, truncateText(tweet.body(), 280)), nil
}

func renderStoredBlueskySummary(data string) (string, error) {
	var item blueskyFeedItem
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		return "", err
	}
	record, err := item.toEvent()
	if err != nil {
		return "", err
	}
	return record.SummaryRendered, nil
}

func renderStoredBlueskyDetail(url, data string) (map[string]any, error) {
	var item blueskyFeedItem
	if err := json.Unmarshal([]byte(data), &item); err != nil {
		return nil, err
	}

	var record blueskyRecord
	if err := json.Unmarshal(item.Post.Record, &record); err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(item.Post.Author.DisplayName)
	handle := strings.TrimSpace(item.Post.Author.Handle)
	if displayName == "" {
		displayName = handle
	}
	if handle != "" && !strings.HasPrefix(handle, "@") {
		handle = "@" + handle
	}

	postURL := url
	if postURL == "" {
		postURL = blueskyPostURL(item.Post.Author.Handle, item.Post.URI)
	}
	ctx := map[string]any{
		"url":          postURL,
		"display_name": displayName,
		"handle":       handle,
		"text":         renderBlueskyFacetText(record.Text, record.Facets),
	}
	if embed := blueskyExternalEmbed(item.Post.URI, record.Embed); embed != nil {
		ctx["embed"] = embed
	}
	return ctx, nil
}

func blueskyExternalEmbed(postURI string, embed *struct {
	Type     string `json:"$type"`
	External *struct {
		URI   string `json:"uri"`
		Title string `json:"title"`
		Thumb *struct {
			Ref struct {
				Link string `json:"$link"`
			} `json:"ref"`
		} `json:"thumb"`
	} `json:"external"`
}) map[string]any {
	if embed == nil || embed.Type != "app.bsky.embed.external" || embed.External == nil {
		return nil
	}
	if strings.TrimSpace(embed.External.URI) == "" || strings.TrimSpace(embed.External.Title) == "" {
		return nil
	}

	domain := embed.External.URI
	if parsed, err := url.Parse(embed.External.URI); err == nil && parsed.Host != "" {
		domain = parsed.Host
	}

	card := map[string]any{
		"url":    embed.External.URI,
		"title":  embed.External.Title,
		"domain": domain,
	}

	did := blueskyPostDID(postURI)
	thumbRef := ""
	if embed.External.Thumb != nil {
		thumbRef = strings.TrimSpace(embed.External.Thumb.Ref.Link)
	}
	if did != "" && thumbRef != "" {
		card["image_url"] = "https://cdn.bsky.app/img/feed_thumbnail/plain/" + did + "/" + thumbRef
	}
	return card
}

func blueskyPostDID(atURI string) string {
	parts := strings.Split(strings.TrimPrefix(atURI, "at://"), "/")
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}

func renderStoredGitHubSummary(url, data string) (string, error) {
	var commitEnvelope struct {
		Kind      string       `json:"kind"`
		Repo      string       `json:"repo"`
		Ref       string       `json:"ref"`
		Commit    githubCommit `json:"commit"`
		CommitURL string       `json:"commit_url"`
	}
	if err := json.Unmarshal([]byte(data), &commitEnvelope); err == nil && commitEnvelope.Kind == "commit" {
		renderURL := commitEnvelope.CommitURL
		if renderURL == "" {
			renderURL = url
		}
		message := firstLine(strings.TrimSpace(commitEnvelope.Commit.Commit.Message))
		if message == "" {
			message = "commit " + shortSHA(commitEnvelope.Commit.SHA)
		}
		branch := strings.TrimPrefix(commitEnvelope.Ref, "refs/heads/")
		return renderGithubSummary(renderURL, commitEnvelope.Repo, githubCommitSummaryText(branch, message)), nil
	}

	var event githubAPIEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return "", err
	}
	switch event.Type {
	case "PushEvent":
		summary, _, _ := event.legacySummary()
		return summary, nil
	case "PullRequestEvent":
		record, err := githubPullRequestItem{Event: event}.ToRecord()
		if err != nil {
			return "", err
		}
		return record.SummaryRendered, nil
	case "IssueCommentEvent":
		record, err := githubIssueCommentItem{Event: event}.ToRecord()
		if err != nil {
			return "", err
		}
		return record.SummaryRendered, nil
	case "PullRequestReviewCommentEvent":
		record, err := githubPullRequestReviewCommentItem{Event: event}.ToRecord()
		if err != nil {
			return "", err
		}
		return record.SummaryRendered, nil
	case "PullRequestReviewEvent":
		record, err := githubPullRequestReviewItem{Event: event}.ToRecord()
		if err != nil {
			return "", err
		}
		return record.SummaryRendered, nil
	default:
		return renderGithubSummary(url, event.Repo.Name, strings.TrimSpace(event.Type)), nil
	}
}

func (e githubAPIEvent) legacySummary() (summary string, url string, title string) {
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
