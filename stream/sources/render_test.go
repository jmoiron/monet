package sources

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderDetailBluesky(t *testing.T) {
	data := `{
		"post": {
			"uri": "at://did:plc:jmoiron/app.bsky.feed.post/3lf7abc",
			"author": {
				"handle": "jmoiron.bsky.social",
				"displayName": "Jason Moiron"
			},
			"record": {
				"$type": "app.bsky.feed.post",
				"text": "hello\nworld",
				"createdAt": "2026-03-19T15:04:05Z",
				"embed": {
					"$type": "app.bsky.embed.external",
					"external": {
						"title": "Example Link",
						"uri": "https://example.com/hello",
						"thumb": {
							"ref": {
								"$link": "bafkrei123"
							}
						}
					}
				},
				"facets": [{
					"features": [{
						"$type": "app.bsky.richtext.facet#link",
						"uri": "https://example.com/hello"
					}],
					"index": {
						"byteStart": 0,
						"byteEnd": 5
					}
				}]
			}
		}
	}`

	templateName, ctx, err := RenderDetail("bluesky", "post", "https://bsky.app/profile/jmoiron.bsky.social/post/3lf7abc", data, "", parseTestTime("2026-03-19T15:04:05Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/bluesky.html", templateName)
	assert.Equal(t, "Jason Moiron", ctx["display_name"])
	assert.Equal(t, "@jmoiron.bsky.social", ctx["handle"])
	assert.Equal(t, "https://bsky.app/profile/jmoiron.bsky.social", ctx["profile_url"])
	assert.Equal(t, "3:04 PM · Mar 19, 2026", ctx["timestamp_ui"])
	assert.Contains(t, ctx["text"], `<a href="https://example.com/hello">hello</a>`)
	assert.Contains(t, ctx["text"], "\nworld")
	assert.True(t, strings.Contains(ctx["url"].(string), "/post/3lf7abc"))
	embed := ctx["embed"].(map[string]any)
	assert.Equal(t, "Example Link", embed["title"])
	assert.Equal(t, "example.com", embed["domain"])
	assert.Equal(t, "https://cdn.bsky.app/img/feed_thumbnail/plain/did:plc:jmoiron/bafkrei123", embed["image_url"])
}

func TestRenderDetailBlueskyImageEmbeds(t *testing.T) {
	data := `{
		"post": {
			"uri": "at://did:plc:jmoiron/app.bsky.feed.post/3img",
			"author": {
				"handle": "jmoiron.bsky.social",
				"displayName": "Jason Moiron"
			},
			"record": {
				"$type": "app.bsky.feed.post",
				"text": "",
				"createdAt": "2026-03-19T15:04:05Z",
				"embed": {
					"$type": "app.bsky.embed.images",
					"images": [{
						"alt": "example alt text",
						"image": {
							"ref": {
								"$link": "bafkreiimg123"
							}
						}
					}]
				}
			}
		}
	}`

	templateName, ctx, err := RenderDetail("bluesky", "post", "https://bsky.app/profile/jmoiron.bsky.social/post/3img", data, "", parseTestTime("2026-03-19T15:04:05Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/bluesky.html", templateName)
	images := ctx["image_embeds"].([]map[string]any)
	require.Len(t, images, 1)
	assert.Equal(t, "example alt text", images[0]["alt"])
	assert.Equal(t, "https://cdn.bsky.app/img/feed_fullsize/plain/did:plc:jmoiron/bafkreiimg123", images[0]["image_url"])
}

func TestRenderDetailGitHubCommit(t *testing.T) {
	data := `{
		"kind": "commit",
		"repo": "jmoiron/monet",
		"ref": "refs/heads/master",
		"commit_url": "https://github.com/jmoiron/monet/commit/abc123",
		"commit": {
			"sha": "abc123",
			"html_url": "https://github.com/jmoiron/monet/commit/abc123",
			"commit": {
				"message": "Improve stream detail render\n\nAdds a custom renderer."
			},
			"author": {
				"login": "jmoiron"
			}
		},
		"source": {
			"actor": {
				"login": "jmoiron",
				"avatar_url": "https://avatars.githubusercontent.com/u/218132?"
			}
		}
	}`

	templateName, ctx, err := RenderDetail("github", "commit", "https://github.com/jmoiron/monet/commit/abc123", data, "", parseTestTime("2026-03-19T15:04:05Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/github.html", templateName)
	assert.Equal(t, "jmoiron/monet", ctx["repo_name"])
	assert.Equal(t, "https://github.com/jmoiron/monet", ctx["repo_url"])
	assert.Equal(t, "master", ctx["branch"])
	assert.Equal(t, "abc123", ctx["short_sha"])
	assert.Equal(t, "@jmoiron", ctx["handle"])
	assert.Equal(t, "https://github.com/jmoiron", ctx["profile_url"])
	assert.Equal(t, "https://avatars.githubusercontent.com/u/218132?", ctx["avatar_url"])
	assert.Equal(t, "3:04 PM · Mar 19, 2026", ctx["timestamp_ui"])
	assert.Contains(t, ctx["message"], "Improve stream detail render")
}

func TestRenderDetailGitHubIssueOpened(t *testing.T) {
	data := `{
		"id":"7007740552",
		"type":"IssuesEvent",
		"actor":{"login":"jmoiron","avatar_url":"https://avatars.githubusercontent.com/u/218132?"},
		"repo":{"name":"jmoiron/quantum-skies"},
		"payload":{
			"action":"opened",
			"issue":{
				"html_url":"https://github.com/jmoiron/quantum-skies/issues/33",
				"number":33,
				"title":"0.8.0 playtesting",
				"body":"- [x] fix #32 in 0.8.1\n- [ ] final pass",
				"user":{
					"login":"jmoiron",
					"html_url":"https://github.com/jmoiron",
					"avatar_url":"https://avatars.githubusercontent.com/u/218132?v=4"
				}
			}
		}
	}`

	templateName, ctx, err := RenderDetail("github", "issue", "https://github.com/jmoiron/quantum-skies/issues/33", data, "", parseTestTime("2026-03-02T15:09:36Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/github-issue.html", templateName)
	assert.Equal(t, "jmoiron/quantum-skies", ctx["repo_name"])
	assert.Equal(t, 33, ctx["issue_number"])
	assert.Equal(t, "@jmoiron", ctx["handle"])
	assert.Equal(t, "https://github.com/jmoiron", ctx["profile_url"])
	assert.Equal(t, "https://avatars.githubusercontent.com/u/218132?v=4", ctx["avatar_url"])
	assert.Equal(t, "0.8.0 playtesting", ctx["issue_title"])
	assert.Contains(t, ctx["issue_body_md"], `<li><input checked="" disabled="" type="checkbox"> fix #32 in 0.8.1</li>`)
}

func TestRenderDetailGitHubPullRequestOpened(t *testing.T) {
	data := `{
		"kind":"pull_request",
		"event":{
			"repo":{"name":"jmoiron/monet"},
			"payload":{"action":"opened"}
		},
		"pull_request":{
			"html_url":"https://github.com/jmoiron/monet/pull/15",
			"number":15,
			"title":"autosaves",
			"body":"Adds an autosave functionality to blog post authoring.\n\nSecond paragraph.",
			"user":{
				"login":"jmoiron",
				"html_url":"https://github.com/jmoiron",
				"avatar_url":"https://avatars.githubusercontent.com/u/218132?v=4"
			},
			"head":{"ref":"autosave"},
			"base":{"ref":"master"}
		}
	}`

	templateName, ctx, err := RenderDetail("github", "pull request", "https://github.com/jmoiron/monet/pull/15", data, "", parseTestTime("2026-02-21T13:26:59Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/github-pr.html", templateName)
	assert.Equal(t, "jmoiron/monet", ctx["repo_name"])
	assert.Equal(t, 15, ctx["pr_number"])
	assert.Equal(t, "@jmoiron", ctx["handle"])
	assert.Equal(t, "https://github.com/jmoiron", ctx["profile_url"])
	assert.Equal(t, "https://avatars.githubusercontent.com/u/218132?v=4", ctx["avatar_url"])
	assert.Equal(t, "autosave", ctx["head_ref"])
	assert.Equal(t, "master", ctx["base_ref"])
	assert.Equal(t, "opened", ctx["action"])
	assert.Equal(t, "autosaves", ctx["pr_title"])
	assert.Equal(t, false, ctx["merged"])
	assert.Contains(t, ctx["pr_body_md"], "<p>Adds an autosave functionality to blog post authoring.</p>")
}

func TestRenderDetailGitHubCreateBranch(t *testing.T) {
	data := `{
		"id":"8991696090",
		"type":"CreateEvent",
		"actor":{"login":"jmoiron","avatar_url":"https://avatars.githubusercontent.com/u/218132?"},
		"repo":{"name":"jmoiron/gcyr"},
		"payload":{
			"ref":"programmable-card",
			"ref_type":"branch"
		}
	}`

	templateName, ctx, err := RenderDetail("github", "create", "https://github.com/jmoiron/gcyr", data, "", parseTestTime("2026-03-03T14:44:38Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/github-create.html", templateName)
	assert.Equal(t, "jmoiron/gcyr", ctx["repo_name"])
	assert.Equal(t, "https://github.com/jmoiron/gcyr", ctx["repo_url"])
	assert.Equal(t, "@jmoiron", ctx["handle"])
	assert.Equal(t, "https://github.com/jmoiron", ctx["profile_url"])
	assert.Equal(t, "https://avatars.githubusercontent.com/u/218132?", ctx["avatar_url"])
	assert.Equal(t, "programmable-card", ctx["ref"])
}

func TestRenderSummaryTwitterArchiveEnvelope(t *testing.T) {
	data := `{
		"profile": {
			"display_name": "Jason Moiron",
			"username": "jmoiron"
		},
		"archive": {
			"tweet": {
				"id_str": "1902761308858890440",
				"created_at": "Thu Mar 20 10:15:00 -0400 2026",
				"full_text": "archive tweet body"
			}
		}
	}`

	summary, err := RenderSummary("twitter", "", data)
	require.NoError(t, err)
	assert.Contains(t, summary, `fa-twitter`)
	assert.Contains(t, summary, `@jmoiron`)
	assert.Contains(t, summary, `archive tweet body`)
}

func TestRenderDetailTwitterArchiveEnvelope(t *testing.T) {
	data := `{
		"profile": {
			"display_name": "Jason Moiron",
			"username": "jmoiron"
		},
		"archive": {
			"tweet": {
				"id_str": "1902761308858890440",
				"created_at": "Thu Mar 20 10:15:00 -0400 2026",
				"full_text": "hello\nworld"
			}
		}
	}`

	templateName, ctx, err := RenderDetail("twitter", "tweet", "", data, "", parseTestTime("2026-03-20T14:15:00Z"))
	require.NoError(t, err)
	assert.Equal(t, "stream/detail/twitter.html", templateName)
	assert.Equal(t, "Jason Moiron", ctx["display_name"])
	assert.Equal(t, "@jmoiron", ctx["handle"])
	assert.Equal(t, "2:15 PM · Mar 20, 2026", ctx["timestamp_ui"])
	assert.Equal(t, "hello\nworld", ctx["text"])
}

func TestRenderSummaryGitHubPullRequestEnvelope(t *testing.T) {
	data := `{
		"kind":"pull_request",
		"event":{
			"repo":{"name":"Argent-Matter/gcyr"},
			"payload":{"action":"merged"}
		},
		"pull_request":{
			"html_url":"https://github.com/Argent-Matter/gcyr/pull/106",
			"number":106,
			"title":"add lang entry for proxima and milkyway",
			"merged":true,
			"head":{"ref":"jm/proxima-lang"},
			"base":{"ref":"1.20.1"}
		}
	}`

	summary, err := RenderSummary("github", "https://github.com/Argent-Matter/gcyr", data)
	require.NoError(t, err)
	assert.Contains(t, summary, "Argent-Matter/gcyr")
	assert.Contains(t, summary, `class="stream-entry-pr"`)
	assert.Contains(t, summary, `href="https://github.com/Argent-Matter/gcyr/pull/106"`)
	assert.Contains(t, summary, `fa-code-merge`)
	assert.Contains(t, summary, ">PR #106<")
	assert.Contains(t, summary, `aria-hidden="true">·</span>`)
	assert.Contains(t, summary, "jm/proxima-lang → 1.20.1")
	assert.NotContains(t, summary, "add lang entry for proxima and milkyway")
	assert.NotContains(t, summary, `<span class="message"></span>`)
}

func TestRenderSummaryGitHubCreateBranch(t *testing.T) {
	data := `{
		"id":"8991696090",
		"type":"CreateEvent",
		"actor":{"login":"jmoiron","avatar_url":"https://avatars.githubusercontent.com/u/218132?"},
		"repo":{"name":"jmoiron/gcyr"},
		"payload":{
			"ref":"programmable-card",
			"ref_type":"branch"
		}
	}`

	summary, err := RenderSummary("github", "https://github.com/jmoiron/gcyr", data)
	require.NoError(t, err)
	assert.Contains(t, summary, "jmoiron/gcyr")
	assert.Contains(t, summary, `fa-code-branch`)
	assert.Contains(t, summary, "→ programmable-card")
}

func TestRenderSummaryGitHubIssueOpened(t *testing.T) {
	data := `{
		"id":"7007740552",
		"type":"IssuesEvent",
		"actor":{"login":"jmoiron","avatar_url":"https://avatars.githubusercontent.com/u/218132?"},
		"repo":{"name":"jmoiron/quantum-skies"},
		"payload":{
			"action":"opened",
			"issue":{
				"html_url":"https://github.com/jmoiron/quantum-skies/issues/33",
				"number":33,
				"title":"0.8.0 playtesting"
			}
		}
	}`

	summary, err := RenderSummary("github", "https://github.com/jmoiron/quantum-skies/issues/33", data)
	require.NoError(t, err)
	assert.Contains(t, summary, "jmoiron/quantum-skies")
	assert.Contains(t, summary, "created")
	assert.Contains(t, summary, `class="stream-entry-issue"`)
	assert.Contains(t, summary, `href="https://github.com/jmoiron/quantum-skies/issues/33"`)
	assert.Contains(t, summary, ">issue #33<")
	assert.NotContains(t, summary, "0.8.0 playtesting")
}

func TestRenderSummaryGitHubClosedPullRequestEnvelopeUsesClosedIcon(t *testing.T) {
	data := `{
		"kind":"pull_request",
		"event":{
			"repo":{"name":"Argent-Matter/gcyr"},
			"payload":{"action":"closed"}
		},
		"pull_request":{
			"html_url":"https://github.com/Argent-Matter/gcyr/pull/107",
			"number":107,
			"title":"close it",
			"merged":false,
			"head":{"ref":"old-branch"},
			"base":{"ref":"1.20.1"}
		}
	}`

	summary, err := RenderSummary("github", "https://github.com/Argent-Matter/gcyr", data)
	require.NoError(t, err)
	assert.Contains(t, summary, `fa-circle-xmark`)
	assert.NotContains(t, summary, `fa-code-merge`)
	assert.NotContains(t, summary, `fa-code-pull-request`)
}

func TestRenderBlueskyFacetText(t *testing.T) {
	facets := []struct {
		Index struct {
			ByteStart int `json:"byteStart"`
			ByteEnd   int `json:"byteEnd"`
		} `json:"index"`
		Features []struct {
			Type string `json:"$type"`
			URI  string `json:"uri"`
		} `json:"features"`
	}{
		{
			Index: struct {
				ByteStart int `json:"byteStart"`
				ByteEnd   int `json:"byteEnd"`
			}{ByteStart: 6, ByteEnd: 10},
			Features: []struct {
				Type string `json:"$type"`
				URI  string `json:"uri"`
			}{
				{Type: "app.bsky.richtext.facet#link", URI: "https://example.com"},
			},
		},
	}

	rendered := renderBlueskyFacetText("hello link world", facets)
	assert.Equal(t, `hello <a href="https://example.com">link</a> world`, rendered)
}

func parseTestTime(value string) time.Time {
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return ts
}
