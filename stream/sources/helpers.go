package sources

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"
)

//go:embed templates/*.html
var summaryTemplates embed.FS

var githubPRSummaryTemplate = template.Must(template.ParseFS(summaryTemplates, "templates/github_pr_summary.html"))

func escape(s string) string {
	return template.HTMLEscapeString(s)
}

func truncateText(s string, limit int) string {
	s = strings.TrimSpace(s)
	rs := []rune(s)
	if len(rs) <= limit {
		return s
	}
	return string(rs[:limit]) + "..."
}

func externalLink(url, text string) string {
	return fmt.Sprintf(`<a href="%s">%s</a>`, escape(url), escape(text))
}

func renderGithubSummary(url, repo, text string) string {
	return fmt.Sprintf(
		`<div class="entry github"><a href="%s"><i class="fa-brands fa-github"></i></a> <span class="name">%s</span> <span class="message">%s</span></div>`,
		escape(url),
		externalLink("https://github.com/"+repo, repo),
		escape(text),
	)
}

func renderGithubSummaryHTML(url, repo, messageHTML string) string {
	return fmt.Sprintf(
		`<div class="entry github"><a href="%s"><i class="fa-brands fa-github"></i></a> <span class="name">%s</span> <span class="message">%s</span></div>`,
		escape(url),
		externalLink("https://github.com/"+repo, repo),
		messageHTML,
	)
}

func renderGithubPRSummary(url, repo, prURL, prText, branchText, action string) string {
	iconClass := ""
	switch strings.TrimSpace(action) {
	case "merged":
		iconClass = "fa-solid fa-code-merge"
	case "opened":
		iconClass = "fa-solid fa-code-pull-request"
	case "closed":
		iconClass = "fa-solid fa-circle-xmark"
	}

	var buf bytes.Buffer
	err := githubPRSummaryTemplate.Execute(&buf, map[string]any{
		"url":         url,
		"repo":        repo,
		"repo_url":    "https://github.com/" + repo,
		"pr_url":      prURL,
		"pr_text":     prText,
		"branch_text": branchText,
		"icon_class":  iconClass,
	})
	if err != nil {
		return renderGithubSummaryHTML(url, repo, `<span class="stream-entry-pr">`+externalLink(prURL, prText)+`</span>`)
	}
	return buf.String()
}

func renderGithubCreateSummary(url, repo, branch string) string {
	messageHTML := `<span aria-hidden="true">·</span> <span class="stream-entry-create"><i class="fa-solid fa-code-branch"></i> ` + escape("→ "+branch) + `</span>`
	return renderGithubSummaryHTML(url, repo, messageHTML)
}

func renderGithubIssueSummary(url, repo, issueURL string, issueNumber int) string {
	messageHTML := `created <span class="stream-entry-issue">` + externalLink(issueURL, fmt.Sprintf("issue #%d", issueNumber)) + `</span>`
	return renderGithubSummaryHTML(url, repo, messageHTML)
}

func renderGithubCommitSummary(url, repo, branch, sha, message string) string {
	parts := []string{}
	if strings.TrimSpace(branch) != "" {
		parts = append(parts, `<span class="stream-entry-commit-branch">`+escape(branch)+`</span>`)
	}
	if strings.TrimSpace(sha) != "" {
		if len(parts) > 0 {
			parts[len(parts)-1] += ` <span class="stream-entry-commit-sha"><i class="fa-solid fa-code-commit"></i> ` + externalLink(url, shortSHA(sha)) + `</span>`
		} else {
			parts = append(parts, `<span class="stream-entry-commit-sha"><i class="fa-solid fa-code-commit"></i> `+externalLink(url, shortSHA(sha))+`</span>`)
		}
	}
	if strings.TrimSpace(message) != "" {
		parts = append(parts, escape(message))
	}
	messageHTML := strings.Join(parts, ` <span aria-hidden="true">·</span> `)
	return renderGithubSummaryHTML(url, repo, messageHTML)
}

func renderBlueskySummary(url, actor, text string) string {
	actorHTML := ""
	if strings.TrimSpace(actor) != "" {
		actorHTML = fmt.Sprintf(`<span class="name">%s</span> `, actor)
	}
	return fmt.Sprintf(
		`<div class="entry bluesky"><a href="%s"><i class="fa-brands fa-bluesky"></i></a> %s<span class="message">%s</span></div>`,
		escape(url),
		actorHTML,
		text,
	)
}

func renderTwitterSummary(url, text string) string {
	return fmt.Sprintf(
		`<div class="entry twitter"><a href="%s"><i class="fa-brands fa-twitter"></i></a> <span class="message">%s</span></div>`,
		escape(url),
		escape(text),
	)
}

func renderTwitterSummaryWithActor(actor, text string) string {
	actorHTML := ""
	if strings.TrimSpace(actor) != "" {
		actorHTML = fmt.Sprintf(`<span class="name">%s</span> `, actor)
	}
	return fmt.Sprintf(
		`<div class="entry twitter"><i class="fa-brands fa-twitter"></i> %s<span class="message">%s</span></div>`,
		actorHTML,
		escape(text),
	)
}
