package sources_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jmoiron/monet/stream"
	"github.com/jmoiron/monet/stream/sources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubSyncExpandsPushEventIntoCommits(t *testing.T) {
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "/users/jmoiron/events/public", r.URL.Path)
			if r.URL.Query().Get("page") != "1" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("[]")),
				}, nil
			}

			body, err := json.Marshal([]map[string]any{
				{
					"id":         "evt-1",
					"type":       "PushEvent",
					"created_at": "2026-03-19T15:04:05Z",
					"repo": map[string]any{
						"name": "jmoiron/monet",
					},
					"payload": map[string]any{
						"ref":    "refs/heads/main",
						"head":   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
						"before": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"size":   2,
						"commits": []map[string]any{
							{
								"sha": "1111111111111111111111111111111111111111",
								"author": map[string]any{
									"login": "jmoiron",
								},
								"commit": map[string]any{
									"message": "first commit\n\nwith more detail",
								},
							},
							{
								"sha": "2222222222222222222222222222222222222222",
								"author": map[string]any{
									"login": "jmoiron",
								},
								"commit": map[string]any{
									"message": "second commit",
								},
								"html_url": "https://github.com/jmoiron/monet/commit/2222222",
							},
						},
					},
				},
			})
			require.NoError(t, err)

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username": "jmoiron",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Items, 2)

	record0, err := result.Items[0].ToRecord()
	require.NoError(t, err)
	record1, err := result.Items[1].ToRecord()
	require.NoError(t, err)

	assert.Equal(t, "commit", record0.Title)
	assert.Equal(t, "1111111111111111111111111111111111111111", record0.SourceId)
	assert.Contains(t, record0.SummaryRendered, "first commit")
	assert.Contains(t, record0.SummaryRendered, `stream-entry-commit-branch`)
	assert.Contains(t, record0.SummaryRendered, ">main<")
	assert.Contains(t, record0.SummaryRendered, `fa-code-commit`)
	assert.Contains(t, record0.SummaryRendered, ">1111111<")
	assert.Equal(t, "https://github.com/jmoiron/monet/commit/1111111111111111111111111111111111111111", record0.Url)

	assert.Equal(t, "https://github.com/jmoiron/monet/commit/2222222", record1.Url)
	assert.Contains(t, record1.SummaryRendered, "second commit")
}

func TestGitHubSyncFallsBackToCompareWhenPushPayloadHasNoCommits(t *testing.T) {
	requests := []string{}
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.Path)

			switch r.URL.Path {
			case "/users/jmoiron/events/public":
				if r.URL.Query().Get("page") != "1" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("[]")),
					}, nil
				}
				body, err := json.Marshal([]map[string]any{
					{
						"id":         "evt-2",
						"type":       "PushEvent",
						"created_at": "2026-03-19T15:04:05Z",
						"repo": map[string]any{
							"name": "jmoiron/monet",
						},
						"payload": map[string]any{
							"ref":     "refs/heads/main",
							"head":    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
							"before":  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
							"size":    1,
							"commits": []map[string]any{},
						},
					},
				})
				require.NoError(t, err)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			case "/repos/jmoiron/monet/compare/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa...bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
				body, err := json.Marshal(map[string]any{
					"commits": []map[string]any{
						{
							"sha":      "3333333333333333333333333333333333333333",
							"html_url": "https://github.com/jmoiron/monet/commit/3333333",
							"author": map[string]any{
								"login": "jmoiron",
							},
							"commit": map[string]any{
								"message": "commit from compare endpoint",
							},
						},
					},
				})
				require.NoError(t, err)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			default:
				t.Fatalf("unexpected request path %s", r.URL.Path)
				return nil, nil
			}
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username": "jmoiron",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	record, err := result.Items[0].ToRecord()
	require.NoError(t, err)
	assert.Equal(t, "3333333333333333333333333333333333333333", record.SourceId)
	assert.Contains(t, record.SummaryRendered, "commit from compare endpoint")
	assert.Equal(t, []string{
		"/users/jmoiron/events/public",
		"/repos/jmoiron/monet/compare/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa...bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"/users/jmoiron/events/public",
	}, requests)
}

func TestGitHubSyncKeepsPRAndCommentEvents(t *testing.T) {
	requests := []string{}
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.Path)
			if r.URL.Path == "/users/jmoiron/events/public" && r.URL.Query().Get("page") != "1" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("[]")),
				}, nil
			}
			if r.URL.Path == "/repos/jmoiron/monet/pulls/10" {
				body, err := json.Marshal(map[string]any{
					"id":       10,
					"html_url": "https://github.com/jmoiron/monet/pull/10",
					"number":   10,
					"title":    "Add better GitHub stream rendering",
					"body":     "This wires commit imports into the stream with full body text.",
				})
				require.NoError(t, err)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			}
			body, err := json.Marshal([]map[string]any{
				{
					"id":         "pr-1",
					"type":       "PullRequestEvent",
					"created_at": "2026-03-19T15:04:05Z",
					"repo":       map[string]any{"name": "jmoiron/monet"},
					"payload": map[string]any{
						"action": "opened",
						"pull_request": map[string]any{
							"url":      "https://api.github.com/repos/jmoiron/monet/pulls/10",
							"html_url": "https://github.com/jmoiron/monet/pull/10",
							"number":   10,
							"title":    "Add better GitHub stream rendering",
							"body":     "This wires commit imports into the stream.",
							"head": map[string]any{
								"ref": "stream-detail",
							},
							"base": map[string]any{
								"ref": "main",
							},
						},
					},
				},
				{
					"id":         "comment-1",
					"type":       "IssueCommentEvent",
					"created_at": "2026-03-19T15:05:05Z",
					"repo":       map[string]any{"name": "jmoiron/monet"},
					"payload": map[string]any{
						"action": "created",
						"issue": map[string]any{
							"html_url": "https://github.com/jmoiron/monet/issues/12",
							"number":   12,
							"title":    "Importer should create commit items",
						},
						"comment": map[string]any{
							"html_url": "https://github.com/jmoiron/monet/issues/12#issuecomment-1",
							"body":     "I think the compare fallback is the right approach.",
						},
					},
				},
			})
			require.NoError(t, err)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username": "jmoiron",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.Len(t, result.Items, 2)

	prRecord, err := result.Items[0].ToRecord()
	require.NoError(t, err)
	assert.Contains(t, prRecord.SummaryRendered, `class="stream-entry-pr"`)
	assert.Contains(t, prRecord.SummaryRendered, `href="https://github.com/jmoiron/monet/pull/10"`)
	assert.Contains(t, prRecord.SummaryRendered, `fa-code-pull-request`)
	assert.Contains(t, prRecord.SummaryRendered, ">PR #10<")
	assert.Contains(t, prRecord.SummaryRendered, `aria-hidden="true">·</span>`)
	assert.Contains(t, prRecord.SummaryRendered, "stream-detail → main")
	assert.NotContains(t, prRecord.SummaryRendered, "Add better GitHub stream rendering")
	assert.Contains(t, prRecord.Data, `"kind":"pull_request"`)
	assert.Contains(t, prRecord.Data, `"pull_request":{"body":"This wires commit imports into the stream with full body text."`)

	commentRecord, err := result.Items[1].ToRecord()
	require.NoError(t, err)
	assert.Contains(t, commentRecord.SummaryRendered, "created comment on issue #12")
	assert.Contains(t, commentRecord.SummaryRendered, "compare fallback")
	assert.Equal(t, []string{
		"/users/jmoiron/events/public",
		"/repos/jmoiron/monet/pulls/10",
		"/users/jmoiron/events/public",
	}, requests)
}

func TestGitHubSyncFiltersPushCommitsByAuthorUsername(t *testing.T) {
	requests := []string{}
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.Path)

			switch r.URL.Path {
			case "/users/jmoiron/events/public":
				if r.URL.Query().Get("page") != "1" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(strings.NewReader("[]")),
					}, nil
				}

				body, err := json.Marshal([]map[string]any{
					{
						"id":         "evt-3",
						"type":       "PushEvent",
						"created_at": "2026-03-19T15:04:05Z",
						"repo": map[string]any{
							"name": "jmoiron/monet",
						},
						"payload": map[string]any{
							"ref":     "refs/heads/main",
							"head":    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
							"before":  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
							"size":    2,
							"commits": []map[string]any{},
						},
					},
				})
				require.NoError(t, err)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			case "/repos/jmoiron/monet/compare/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa...bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb":
				body, err := json.Marshal(map[string]any{
					"commits": []map[string]any{
						{
							"sha":      "4444444444444444444444444444444444444444",
							"html_url": "https://github.com/jmoiron/monet/commit/4444444",
							"author": map[string]any{
								"login": "jmoiron",
							},
							"commit": map[string]any{
								"message": "my commit",
							},
						},
						{
							"sha":      "5555555555555555555555555555555555555555",
							"html_url": "https://github.com/jmoiron/monet/commit/5555555",
							"author": map[string]any{
								"login": "upstream-user",
							},
							"commit": map[string]any{
								"message": "someone else's commit",
							},
						},
					},
				})
				require.NoError(t, err)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			default:
				t.Fatalf("unexpected request path %s", r.URL.Path)
				return nil, nil
			}
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username": "jmoiron",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.Len(t, result.Items, 1)

	record, err := result.Items[0].ToRecord()
	require.NoError(t, err)
	assert.Equal(t, "4444444444444444444444444444444444444444", record.SourceId)
	assert.Contains(t, record.SummaryRendered, "my commit")
	assert.NotContains(t, record.SummaryRendered, "someone else's commit")
	assert.Equal(t, []string{
		"/users/jmoiron/events/public",
		"/repos/jmoiron/monet/compare/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa...bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"/users/jmoiron/events/public",
	}, requests)
}

func TestGitHubSyncStoresEventsETagFromFirstPage(t *testing.T) {
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "/users/jmoiron/events/public", r.URL.Path)
			require.Empty(t, r.Header.Get("If-None-Match"))

			if r.URL.Query().Get("page") != "1" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("[]")),
				}, nil
			}

			body, err := json.Marshal([]map[string]any{})
			require.NoError(t, err)
			header := make(http.Header)
			header.Set("ETag", `"events-page-1"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username": "jmoiron",
		"use_etag": "true",
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, map[string]string{"events_etag": `"events-page-1"`}, result.SettingsUpdates)
}

func TestGitHubSyncUsesIfNoneMatchAndStopsOnNotModified(t *testing.T) {
	requests := 0
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			require.Equal(t, "/users/jmoiron/events/public", r.URL.Path)
			require.Equal(t, `"events-page-1"`, r.Header.Get("If-None-Match"))
			require.Equal(t, "Bearer github-token", r.Header.Get("Authorization"))
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username":    "jmoiron",
		"token":       "github-token",
		"use_etag":    "true",
		"events_etag": `"events-page-1"`,
	}))

	result, err := module.Sync(context.Background(), source)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Items)
	assert.Equal(t, true, result.Details["not_modified"])
	assert.Equal(t, 1, requests)
}

func TestGitHubFullSyncBypassesIfNoneMatch(t *testing.T) {
	requests := 0
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			require.Equal(t, "/users/jmoiron/events/public", r.URL.Path)
			require.Empty(t, r.Header.Get("If-None-Match"))

			if r.URL.Query().Get("page") != "1" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("[]")),
				}, nil
			}

			body, err := json.Marshal([]map[string]any{
				{
					"id":         "evt-full-1",
					"type":       "PushEvent",
					"created_at": "2026-03-19T15:04:05Z",
					"repo":       map[string]any{"name": "jmoiron/monet"},
					"payload": map[string]any{
						"ref":    "refs/heads/main",
						"head":   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
						"before": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"size":   1,
						"commits": []map[string]any{
							{
								"sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
								"author": map[string]any{
									"login": "jmoiron",
								},
								"commit": map[string]any{
									"message": "full sync commit",
								},
							},
						},
					},
				},
			})
			require.NoError(t, err)

			header := make(http.Header)
			header.Set("ETag", `"fresh-etag"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username":    "jmoiron",
		"token":       "github-token",
		"use_etag":    "true",
		"events_etag": `"old-etag"`,
	}))

	result, err := module.Sync(stream.WithSyncMode(context.Background(), stream.SyncModeFull), source)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Items, 1)
	assert.Equal(t, `"fresh-etag"`, result.SettingsUpdates["events_etag"])
	assert.Equal(t, "full", result.Details["sync_mode"])
	assert.Nil(t, result.Details["not_modified"])
	assert.Equal(t, 2, requests)
}

func TestGitHubSyncPageOverrideFetchesOnlyRequestedPage(t *testing.T) {
	requests := []string{}
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.RawQuery)
			require.Equal(t, "/users/jmoiron/events/public", r.URL.Path)
			require.Empty(t, r.Header.Get("If-None-Match"))

			if r.URL.Query().Get("page") != "5" {
				t.Fatalf("unexpected page %s", r.URL.Query().Get("page"))
			}

			body, err := json.Marshal([]map[string]any{
				{
					"id":         "evt-page-5",
					"type":       "PushEvent",
					"created_at": "2025-01-19T15:04:05Z",
					"repo":       map[string]any{"name": "jmoiron/monet"},
					"payload": map[string]any{
						"ref":    "refs/heads/main",
						"head":   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
						"before": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						"size":   1,
						"commits": []map[string]any{
							{
								"sha": "555555555555555555555555555555555555555a",
								"author": map[string]any{
									"login": "jmoiron",
								},
								"commit": map[string]any{
									"message": "older backfill commit",
								},
							},
						},
					},
				},
			})
			require.NoError(t, err)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(body))),
			}, nil
		}),
	})

	source := &stream.StreamSource{}
	require.NoError(t, source.SetSettings(map[string]string{
		"username":    "jmoiron",
		"token":       "github-token",
		"use_etag":    "true",
		"events_etag": `"page-1-etag"`,
	}))

	result, err := module.Sync(stream.WithPageOverride(context.Background(), 5), source)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Items, 1)
	assert.Equal(t, 5, result.Details["page"])
	assert.Equal(t, "incremental", result.Details["sync_mode"])
	assert.Nil(t, result.SettingsUpdates)
	assert.Equal(t, []string{"per_page=100&page=5"}, requests)

	record, err := result.Items[0].ToRecord()
	require.NoError(t, err)
	assert.Contains(t, record.SummaryRendered, "older backfill commit")
}
