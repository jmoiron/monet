package sources

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jmoiron/monet/mtr"
)

type Evaluation struct {
	SummaryRendered string
	Hidden          bool
}

func Reevaluate(eventType, url, data string, settings map[string]string) (*Evaluation, error) {
	summary, hidden, err := renderAndFilter(eventType, url, data, settings)
	if err != nil {
		return nil, err
	}
	return &Evaluation{
		SummaryRendered: summary,
		Hidden:          hidden,
	}, nil
}

func RenderSummary(eventType, url, data string) (string, error) {
	evaluation, err := Reevaluate(eventType, url, data, nil)
	if err != nil {
		return "", err
	}
	return evaluation.SummaryRendered, nil
}

func RenderDetail(eventType, title, url, data, summaryRendered string, timestamp time.Time) (string, map[string]any, error) {
	switch eventType {
	case "bluesky":
		ctx, err := renderStoredBlueskyDetail(url, data, timestamp)
		return "stream/detail/bluesky.html", ctx, err
	case "github":
		templateName, ctx, err := renderStoredGitHubDetail(title, url, data, summaryRendered, timestamp)
		return templateName, ctx, err
	case "twitter":
		ctx, err := renderStoredTwitterDetail(data, timestamp)
		return "stream/detail/twitter.html", ctx, err
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

func renderAndFilter(eventType, url, data string, settings map[string]string) (string, bool, error) {
	switch eventType {
	case "github":
		return renderStoredGitHubSummary(url, data, settings)
	case "bluesky":
		summary, err := renderStoredBlueskySummary(data)
		return summary, false, err
	case "twitter":
		summary, err := renderStoredTwitterSummary(url, data)
		return summary, false, err
	default:
		return "", false, fmt.Errorf("unsupported event type %q", eventType)
	}
}

func renderStoredTwitterSummary(url, data string) (string, error) {
	profile, _ := twitterStoredProfile(data)
	body, err := twitterSummaryBody(data)
	if err != nil {
		return "", err
	}
	actor := ""
	if profile != nil && profile.Username != "" {
		actor = "@" + profile.Username
	}
	return renderTwitterSummaryWithActor(actor, truncateText(body, 280)), nil
}

type twitterProfile struct {
	DisplayName string `json:"display_name"`
	Username    string `json:"username"`
}

func twitterStoredProfile(data string) (*twitterProfile, bool) {
	var envelope struct {
		Profile twitterProfile `json:"profile"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err == nil {
		if strings.TrimSpace(envelope.Profile.DisplayName) != "" || strings.TrimSpace(envelope.Profile.Username) != "" {
			return &envelope.Profile, true
		}
	}
	return nil, false
}

func twitterSummaryBody(data string) (string, error) {
	var merged struct {
		Profile twitterProfile  `json:"profile"`
		Legacy  json.RawMessage `json:"legacy"`
		Archive json.RawMessage `json:"archive"`
	}
	if err := json.Unmarshal([]byte(data), &merged); err == nil && (len(merged.Legacy) > 0 || len(merged.Archive) > 0) {
		if len(merged.Archive) > 0 {
			if body, ok := twitterSummaryBodyFromRaw(merged.Archive); ok {
				return body, nil
			}
		}
		if len(merged.Legacy) > 0 {
			if body, ok := twitterSummaryBodyFromRaw(merged.Legacy); ok {
				return body, nil
			}
		}
	}

	if body, ok := twitterSummaryBodyFromRaw(json.RawMessage(data)); ok {
		return body, nil
	}
	return "", fmt.Errorf("unsupported twitter event payload")
}

func renderStoredTwitterDetail(data string, timestamp time.Time) (map[string]any, error) {
	profile, _ := twitterStoredProfile(data)
	body, err := twitterSummaryBody(data)
	if err != nil {
		return nil, err
	}

	displayName := ""
	handle := ""
	if profile != nil {
		displayName = strings.TrimSpace(profile.DisplayName)
		handle = strings.TrimSpace(profile.Username)
	}
	if displayName == "" {
		displayName = handle
	}
	if handle != "" && !strings.HasPrefix(handle, "@") {
		handle = "@" + handle
	}

	return map[string]any{
		"display_name": displayName,
		"handle":       handle,
		"text":         body,
		"timestamp_ui": timestamp.Format("3:04 PM") + " · " + timestamp.Format("Jan 2, 2006"),
	}, nil
}

func twitterSummaryBodyFromRaw(raw json.RawMessage) (string, bool) {
	var entry twitterArchiveEntry
	if err := json.Unmarshal(raw, &entry); err == nil && strings.TrimSpace(entry.Tweet.IDStr) != "" {
		return entry.Tweet.body(), true
	}

	var tweet twitterArchiveTweet
	if err := json.Unmarshal(raw, &tweet); err == nil && strings.TrimSpace(tweet.IDStr) != "" {
		return tweet.body(), true
	}
	return "", false
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

func renderStoredBlueskyDetail(url, data string, timestamp time.Time) (map[string]any, error) {
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
		"profile_url":  "https://bsky.app/profile/" + item.Post.Author.Handle,
		"timestamp_ui": timestamp.Format("3:04 PM") + " · " + timestamp.Format("Jan 2, 2006"),
		"display_name": displayName,
		"handle":       handle,
		"text":         renderBlueskyFacetText(record.Text, record.Facets),
	}
	if embed := blueskyExternalEmbed(item.Post.URI, record.Embed); embed != nil {
		ctx["embed"] = embed
	}
	if images := blueskyImageEmbeds(item.Post.URI, record.Embed); len(images) > 0 {
		ctx["image_embeds"] = images
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
	Images []struct {
		Alt   string `json:"alt"`
		Image struct {
			Ref struct {
				Link string `json:"$link"`
			} `json:"ref"`
		} `json:"image"`
	} `json:"images"`
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

func blueskyImageEmbeds(postURI string, embed *struct {
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
	Images []struct {
		Alt   string `json:"alt"`
		Image struct {
			Ref struct {
				Link string `json:"$link"`
			} `json:"ref"`
		} `json:"image"`
	} `json:"images"`
}) []map[string]any {
	if embed == nil || embed.Type != "app.bsky.embed.images" || len(embed.Images) == 0 {
		return nil
	}

	did := blueskyPostDID(postURI)
	if did == "" {
		return nil
	}

	out := make([]map[string]any, 0, len(embed.Images))
	for _, image := range embed.Images {
		ref := strings.TrimSpace(image.Image.Ref.Link)
		if ref == "" {
			continue
		}
		imageURL := "https://cdn.bsky.app/img/feed_fullsize/plain/" + did + "/" + ref
		out = append(out, map[string]any{
			"url":       imageURL,
			"image_url": imageURL,
			"alt":       image.Alt,
		})
	}
	return out
}

func blueskyPostDID(atURI string) string {
	parts := strings.Split(strings.TrimPrefix(atURI, "at://"), "/")
	if len(parts) < 1 {
		return ""
	}
	return parts[0]
}

func renderStoredGitHubSummary(url, data string, settings map[string]string) (string, bool, error) {
	username := strings.TrimSpace(settings["username"])
	var pullRequestEnvelope struct {
		Kind  string `json:"kind"`
		Event struct {
			Repo struct {
				Name string `json:"name"`
			} `json:"repo"`
			Payload struct {
				Action string `json:"action"`
			} `json:"payload"`
		} `json:"event"`
		PullRequest struct {
			HTMLURL string `json:"html_url"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
			Merged  bool   `json:"merged"`
			Head    struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal([]byte(data), &pullRequestEnvelope); err == nil && pullRequestEnvelope.Kind == "pull_request" {
		renderURL := pullRequestEnvelope.PullRequest.HTMLURL
		if renderURL == "" {
			renderURL = url
		}
		return githubPullRequestSummaryRendered(
			renderURL,
			pullRequestEnvelope.Event.Repo.Name,
			pullRequestEnvelope.PullRequest.Number,
			pullRequestEnvelope.PullRequest.Head.Ref,
			pullRequestEnvelope.PullRequest.Base.Ref,
			pullRequestEnvelope.Event.Payload.Action,
			pullRequestEnvelope.PullRequest.Merged,
		), false, nil
	}

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
		hidden := username != "" && !githubCommitMatchesUsername(commitEnvelope.Commit, username)
		return renderGithubCommitSummary(renderURL, commitEnvelope.Repo, branch, commitEnvelope.Commit.SHA, message), hidden, nil
	}

	var event githubAPIEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return "", false, err
	}
	switch event.Type {
	case "PushEvent":
		var payload struct {
			Commits []json.RawMessage `json:"commits"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err == nil && len(payload.Commits) == 0 {
			summary, _, _ := event.legacySummary()
			return summary, true, nil
		}
		summary, _, _ := event.legacySummary()
		return summary, false, nil
	case "PullRequestEvent":
		record, err := githubPullRequestItem{Event: event}.ToRecord()
		if err != nil {
			return "", false, err
		}
		return record.SummaryRendered, false, nil
	case "CreateEvent":
		var payload struct {
			RefType string `json:"ref_type"`
			Ref     string `json:"ref"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(payload.RefType) == "branch" && strings.TrimSpace(payload.Ref) != "" {
			return renderGithubCreateSummary(url, event.Repo.Name, payload.Ref), false, nil
		}
		summary, _, _ := event.legacySummary()
		return summary, false, nil
	case "IssuesEvent":
		var payload struct {
			Action string `json:"action"`
			Issue  struct {
				HTMLURL string `json:"html_url"`
				Number  int    `json:"number"`
			} `json:"issue"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return "", false, err
		}
		if strings.TrimSpace(payload.Action) == "opened" && payload.Issue.Number > 0 && strings.TrimSpace(payload.Issue.HTMLURL) != "" {
			return renderGithubIssueSummary(url, event.Repo.Name, payload.Issue.HTMLURL, payload.Issue.Number), false, nil
		}
		summary, _, _ := event.legacySummary()
		return summary, false, nil
	case "IssueCommentEvent":
		record, err := githubIssueCommentItem{Event: event}.ToRecord()
		if err != nil {
			return "", false, err
		}
		return record.SummaryRendered, false, nil
	case "PullRequestReviewCommentEvent":
		record, err := githubPullRequestReviewCommentItem{Event: event}.ToRecord()
		if err != nil {
			return "", false, err
		}
		return record.SummaryRendered, false, nil
	case "PullRequestReviewEvent":
		record, err := githubPullRequestReviewItem{Event: event}.ToRecord()
		if err != nil {
			return "", false, err
		}
		return record.SummaryRendered, false, nil
	default:
		return renderGithubSummary(url, event.Repo.Name, strings.TrimSpace(event.Type)), false, nil
	}
}

func renderStoredGitHubDetail(title, url, data, summaryRendered string, timestamp time.Time) (string, map[string]any, error) {
	var pullRequestEnvelope struct {
		Kind  string `json:"kind"`
		Event struct {
			Repo struct {
				Name string `json:"name"`
			} `json:"repo"`
			Payload struct {
				Action string `json:"action"`
			} `json:"payload"`
		} `json:"event"`
		PullRequest struct {
			HTMLURL string `json:"html_url"`
			Number  int    `json:"number"`
			Title   string `json:"title"`
			Body    string `json:"body"`
			Merged  bool   `json:"merged"`
			User    struct {
				Login     string `json:"login"`
				HTMLURL   string `json:"html_url"`
				AvatarURL string `json:"avatar_url"`
			} `json:"user"`
			Head struct {
				Ref string `json:"ref"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal([]byte(data), &pullRequestEnvelope); err == nil && pullRequestEnvelope.Kind == "pull_request" {
		handle := strings.TrimSpace(pullRequestEnvelope.PullRequest.User.Login)
		if handle != "" {
			handle = "@" + handle
		}
		profileURL := pullRequestEnvelope.PullRequest.User.HTMLURL
		if profileURL == "" && pullRequestEnvelope.PullRequest.User.Login != "" {
			profileURL = "https://github.com/" + pullRequestEnvelope.PullRequest.User.Login
		}
		prURL := pullRequestEnvelope.PullRequest.HTMLURL
		if prURL == "" {
			prURL = url
		}
		repoName := pullRequestEnvelope.Event.Repo.Name
		repoURL := ""
		if strings.TrimSpace(repoName) != "" {
			repoURL = "https://github.com/" + repoName
		}

		return "stream/detail/github-pr.html", map[string]any{
			"url":          prURL,
			"repo_name":    repoName,
			"repo_url":     repoURL,
			"pr_number":    pullRequestEnvelope.PullRequest.Number,
			"profile_url":  profileURL,
			"handle":       handle,
			"avatar_url":   pullRequestEnvelope.PullRequest.User.AvatarURL,
			"timestamp_ui": timestamp.Format("3:04 PM") + " · " + timestamp.Format("Jan 2, 2006"),
			"pr_title":     pullRequestEnvelope.PullRequest.Title,
			"pr_body_md":   mtr.RenderMarkdown(pullRequestEnvelope.PullRequest.Body),
			"head_ref":     pullRequestEnvelope.PullRequest.Head.Ref,
			"base_ref":     pullRequestEnvelope.PullRequest.Base.Ref,
			"action":       pullRequestEnvelope.Event.Payload.Action,
			"merged":       pullRequestEnvelope.PullRequest.Merged,
		}, nil
	}

	var commitEnvelope struct {
		Kind   string `json:"kind"`
		Repo   string `json:"repo"`
		Ref    string `json:"ref"`
		Commit struct {
			SHA     string `json:"sha"`
			HTMLURL string `json:"html_url"`
			Commit  struct {
				Message string `json:"message"`
			} `json:"commit"`
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"commit"`
		CommitURL string `json:"commit_url"`
		Source    struct {
			Actor struct {
				Login     string `json:"login"`
				AvatarURL string `json:"avatar_url"`
			} `json:"actor"`
		} `json:"source"`
	}
	if err := json.Unmarshal([]byte(data), &commitEnvelope); err == nil && commitEnvelope.Kind == "commit" {
		commitURL := commitEnvelope.CommitURL
		if commitURL == "" {
			commitURL = commitEnvelope.Commit.HTMLURL
		}
		if commitURL == "" {
			commitURL = url
		}

		login := strings.TrimSpace(commitEnvelope.Source.Actor.Login)
		if login == "" {
			login = strings.TrimSpace(commitEnvelope.Commit.Author.Login)
		}
		profileURL := ""
		if login != "" {
			profileURL = "https://github.com/" + login
			login = "@" + login
		}

		repoURL := ""
		if strings.TrimSpace(commitEnvelope.Repo) != "" {
			repoURL = "https://github.com/" + commitEnvelope.Repo
		}

		return "stream/detail/github.html", map[string]any{
			"url":          commitURL,
			"repo_name":    commitEnvelope.Repo,
			"repo_url":     repoURL,
			"branch":       strings.TrimPrefix(commitEnvelope.Ref, "refs/heads/"),
			"short_sha":    shortSHA(commitEnvelope.Commit.SHA),
			"profile_url":  profileURL,
			"handle":       login,
			"avatar_url":   commitEnvelope.Source.Actor.AvatarURL,
			"timestamp_ui": timestamp.Format("3:04 PM") + " · " + timestamp.Format("Jan 2, 2006"),
			"message":      commitEnvelope.Commit.Commit.Message,
		}, nil
	}

	var event githubAPIEvent
	if err := json.Unmarshal([]byte(data), &event); err == nil && event.Type == "IssuesEvent" {
		var payload struct {
			Action string `json:"action"`
			Issue  struct {
				HTMLURL string `json:"html_url"`
				Number  int    `json:"number"`
				Title   string `json:"title"`
				Body    string `json:"body"`
				User    struct {
					Login     string `json:"login"`
					HTMLURL   string `json:"html_url"`
					AvatarURL string `json:"avatar_url"`
				} `json:"user"`
			} `json:"issue"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err == nil && payload.Action == "opened" {
			handle := strings.TrimSpace(payload.Issue.User.Login)
			if handle != "" {
				handle = "@" + handle
			}
			profileURL := payload.Issue.User.HTMLURL
			if profileURL == "" && payload.Issue.User.Login != "" {
				profileURL = "https://github.com/" + payload.Issue.User.Login
			}
			issueURL := payload.Issue.HTMLURL
			if issueURL == "" {
				issueURL = url
			}
			repoURL := ""
			if strings.TrimSpace(event.Repo.Name) != "" {
				repoURL = "https://github.com/" + event.Repo.Name
			}

			return "stream/detail/github-issue.html", map[string]any{
				"url":           issueURL,
				"repo_name":     event.Repo.Name,
				"repo_url":      repoURL,
				"issue_number":  payload.Issue.Number,
				"profile_url":   profileURL,
				"handle":        handle,
				"avatar_url":    payload.Issue.User.AvatarURL,
				"timestamp_ui":  timestamp.Format("3:04 PM") + " · " + timestamp.Format("Jan 2, 2006"),
				"issue_title":   payload.Issue.Title,
				"issue_body_md": mtr.RenderMarkdown(payload.Issue.Body),
			}, nil
		}
	}

	if err := json.Unmarshal([]byte(data), &event); err == nil && event.Type == "CreateEvent" {
		var payload struct {
			RefType string `json:"ref_type"`
			Ref     string `json:"ref"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err == nil && payload.RefType == "branch" {
			handle := strings.TrimSpace(event.Actor.Login)
			if handle != "" {
				handle = "@" + handle
			}
			profileURL := ""
			if event.Actor.Login != "" {
				profileURL = "https://github.com/" + event.Actor.Login
			}
			repoURL := ""
			if strings.TrimSpace(event.Repo.Name) != "" {
				repoURL = "https://github.com/" + event.Repo.Name
			}
			return "stream/detail/github-create.html", map[string]any{
				"url":          url,
				"repo_name":    event.Repo.Name,
				"repo_url":     repoURL,
				"profile_url":  profileURL,
				"handle":       handle,
				"avatar_url":   event.Actor.AvatarURL,
				"timestamp_ui": timestamp.Format("3:04 PM") + " · " + timestamp.Format("Jan 2, 2006"),
				"ref_type":     payload.RefType,
				"ref":          payload.Ref,
			}, nil
		}
	}

	return "stream/detail/default.html", map[string]any{
		"title":            title,
		"url":              url,
		"summary_rendered": summaryRendered,
	}, nil
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
