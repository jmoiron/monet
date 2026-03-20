package sources

import (
	"fmt"
	"html/template"
	"strings"
)

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

func renderBlueskySummary(url, actor, text string) string {
	return fmt.Sprintf(
		`<div class="entry bluesky"><a href="%s"><i class="fa-brands fa-bluesky"></i></a> <span class="name">%s</span> <span class="message">%s</span></div>`,
		escape(url),
		escape(actor),
		escape(text),
	)
}

func renderTwitterSummary(url, text string) string {
	return fmt.Sprintf(
		`<div class="entry twitter"><a href="%s"><i class="fa-brands fa-twitter"></i></a> <span class="message">%s</span></div>`,
		escape(url),
		escape(text),
	)
}
