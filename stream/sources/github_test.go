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
								"commit": map[string]any{
									"message": "first commit\n\nwith more detail",
								},
							},
							{
								"sha": "2222222222222222222222222222222222222222",
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
	assert.Contains(t, record0.SummaryRendered, "(main)")
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
	module := sources.NewGitHub().WithClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/users/jmoiron/events/public" && r.URL.Query().Get("page") != "1" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("[]")),
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
							"html_url": "https://github.com/jmoiron/monet/pull/10",
							"number":   10,
							"title":    "Add better GitHub stream rendering",
							"body":     "This wires commit imports into the stream.",
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
	assert.Contains(t, prRecord.SummaryRendered, "opened PR #10")
	assert.Contains(t, prRecord.SummaryRendered, "This wires commit imports")

	commentRecord, err := result.Items[1].ToRecord()
	require.NoError(t, err)
	assert.Contains(t, commentRecord.SummaryRendered, "created comment on issue #12")
	assert.Contains(t, commentRecord.SummaryRendered, "compare fallback")
}
