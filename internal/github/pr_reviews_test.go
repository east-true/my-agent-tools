package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func reviewFixturePR(w http.ResponseWriter, head string, threads []graphReviewThread, info reviewPageInfo) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
		"url": "https://github.com/owner/repo/pull/7", "head_sha": head, "review_decision": "CHANGES_REQUESTED",
		"reviewThreads": map[string]any{"nodes": threads, "pageInfo": info},
	}}}})
}

func reviewFixtureThread(id string, resolved, outdated bool, comments ...ReviewComment) graphReviewThread {
	line := 12
	thread := graphReviewThread{ReviewThread: ReviewThread{ID: id, Path: "main.go", Line: &line, OriginalLine: &line, DiffSide: "RIGHT", Resolved: resolved, Outdated: outdated}, Connection: &reviewComments{Nodes: comments}}
	if outdated {
		thread.Line = nil
	}
	return thread
}

func reviewFixtureHistory(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/repos/owner/repo/pulls/7/reviews":
		fmt.Fprint(w, `[{"id":1,"state":"CHANGES_REQUESTED","body":"검증을 추가해주세요","user":{"login":"reviewer"},"commit_id":"abc","html_url":"https://github.com/owner/repo/pull/7#pullrequestreview-1"},{"id":2,"state":"DISMISSED","body":"old request"},{"id":3,"state":"PENDING","body":"unsubmitted"}]`)
	case "/repos/owner/repo/pulls/7":
		fmt.Fprint(w, `{"head":{"sha":"abc"}}`)
	default:
		return false
	}
	return true
}

func TestReviewsPaginateThreadsRepliesAndSubmittedHistory(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			body := "요청 사항\n`literal` $(preserve)\n" + strings.Repeat("context\n", 1000)
			first := reviewFixtureThread("T1", false, false, ReviewComment{ID: "C1", Body: body, Author: &ReviewAuthor{Login: "reviewer"}})
			first.Connection.PageInfo = reviewPageInfo{HasNextPage: true, EndCursor: "comments-1"}
			nestedReads, threadReads := 0, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					var payload struct {
						Query string         `json:"query"`
						Vars  map[string]any `json:"variables"`
					}
					_ = json.NewDecoder(r.Body).Decode(&payload)
					if strings.HasPrefix(payload.Query, "query ReviewThreadComments") {
						nestedReads++
						if payload.Vars["id"] != "T1" || payload.Vars["cursor"] != "comments-1" {
							t.Errorf("nested variables: %+v", payload.Vars)
						}
						fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[{"id":"C2","author":null,"body":"reply","url":"https://github.com/owner/repo/pull/7#discussion_r2"}],"pageInfo":{"hasNextPage":false}}}}}`)
						return
					}
					threadReads++
					if payload.Vars["cursor"] == nil {
						reviewFixturePR(w, "abc", []graphReviewThread{first, reviewFixtureThread("T2", true, false)}, reviewPageInfo{HasNextPage: true, EndCursor: "threads-1"})
					} else {
						if payload.Vars["cursor"] != "threads-1" {
							t.Errorf("thread variables: %+v", payload.Vars)
						}
						reviewFixturePR(w, "abc", []graphReviewThread{reviewFixtureThread("T3", false, true, ReviewComment{ID: "C3", Body: "outdated request"})}, reviewPageInfo{})
					}
					return
				}
				if r.URL.Path == "/repos/owner/repo/pulls/7/reviews" && r.URL.Query().Get("page") == "1" {
					w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/pulls/7/reviews?page=2>; rel="next"`, r.Host))
				}
				if r.URL.Path == "/repos/owner/repo/pulls/7/reviews" && r.URL.Query().Get("page") == "2" {
					fmt.Fprint(w, `[{"id":4,"state":"APPROVED","body":"approved"}]`)
					return
				}
				if !reviewFixtureHistory(w, r) {
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(500)
				}
			})
			result, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7, All: all})
			wantThreads := 2
			if all {
				wantThreads = 3
			}
			if err != nil || result.Status != "ok" || !result.Complete || len(result.Threads) != wantThreads || len(result.Reviews) != 3 || nestedReads != 1 || threadReads != 2 {
				t.Fatalf("result=%+v err=%v nested=%d pages=%d", result, err, nestedReads, threadReads)
			}
			if len(result.Threads[0].Comments) != 2 || result.Threads[0].Comments[0].Body != body || result.Threads[0].Comments[1].Author != nil {
				t.Fatalf("comments not preserved: %+v", result.Threads[0])
			}
			outdated := result.Threads[len(result.Threads)-1]
			if !outdated.Outdated || outdated.Line != nil || outdated.OriginalLine == nil || *outdated.OriginalLine != 12 {
				t.Fatalf("outdated location lost: %+v", outdated)
			}
			if len(f.writes()) != 0 {
				t.Fatal("review collection mutated GitHub")
			}
		})
	}
}

func TestReviewsPartialCollectionPreservesAvailableComments(t *testing.T) {
	for _, failure := range []string{"nested error", "nested missing", "nested cursor", "thread page error", "thread cursor", "review history", "head changed", "head unavailable", "duplicate thread"} {
		t.Run(failure, func(t *testing.T) {
			thread := reviewFixtureThread("T1", false, false, ReviewComment{ID: "C1", Body: "retained"})
			if strings.HasPrefix(failure, "nested") {
				thread.Connection.PageInfo = reviewPageInfo{HasNextPage: true, EndCursor: "c1"}
			}
			reads := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					var payload map[string]any
					_ = json.NewDecoder(r.Body).Decode(&payload)
					query := payload["query"].(string)
					if strings.Contains(query, "query ReviewThreadComments") {
						switch failure {
						case "nested error":
							fmt.Fprint(w, `{"errors":[{"message":"denied"}],"data":{"node":null}}`)
						case "nested missing":
							fmt.Fprint(w, `{"data":{"node":null}}`)
						case "nested cursor":
							fmt.Fprint(w, `{"data":{"node":{"comments":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"c1"}}}}}`)
						}
						return
					}
					reads++
					info := reviewPageInfo{}
					if strings.HasPrefix(failure, "thread") {
						info = reviewPageInfo{HasNextPage: true, EndCursor: "t1"}
						if reads > 1 && failure == "thread page error" {
							w.WriteHeader(403)
							fmt.Fprint(w, `{"message":"denied"}`)
							return
						}
					}
					threads := []graphReviewThread{thread}
					if failure == "duplicate thread" {
						threads = append(threads, thread)
					}
					reviewFixturePR(w, "abc", threads, info)
					return
				}
				if failure == "review history" && strings.HasSuffix(r.URL.Path, "/reviews") {
					w.WriteHeader(403)
					fmt.Fprint(w, `{"message":"denied"}`)
					return
				}
				if r.URL.Path == "/repos/owner/repo/pulls/7" {
					if failure == "head changed" {
						fmt.Fprint(w, `{"head":{"sha":"new"}}`)
						return
					}
					if failure == "head unavailable" {
						w.WriteHeader(500)
						fmt.Fprint(w, `{"message":"unavailable"}`)
						return
					}
				}
				if !reviewFixtureHistory(w, r) {
					t.Errorf("unexpected request %s", r.URL)
				}
			})
			result, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7})
			if err != nil || result.Status != "partial" || result.Complete || len(result.Notes) == 0 || len(result.Threads) != 1 || result.Threads[0].Comments[0].Body != "retained" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestReviewsEmptyResultsAndInitialErrors(t *testing.T) {
	for _, mode := range []string{"empty", "missing", "graphql error", "incomplete", "http error"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/graphql" {
					if r.URL.Path == "/repos/owner/repo/pulls/7/reviews" {
						fmt.Fprint(w, `[]`)
					} else {
						reviewFixtureHistory(w, r)
					}
					return
				}
				switch mode {
				case "empty":
					reviewFixturePR(w, "abc", []graphReviewThread{}, reviewPageInfo{})
				case "missing":
					fmt.Fprint(w, `{"data":{"repository":{"pullRequest":null}}}`)
				case "graphql error":
					fmt.Fprint(w, `{"errors":[{"message":"not authorized"}]}`)
				case "incomplete":
					fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"url":"url","head_sha":"abc"}}}}`)
				case "http error":
					w.WriteHeader(404)
					fmt.Fprint(w, `{"message":"not found"}`)
				}
			})
			result, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7})
			if mode != "empty" {
				if err == nil || len(f.requests) != 1 {
					t.Fatalf("error=%v requests=%+v", err, f.requests)
				}
				return
			}
			data, _ := json.Marshal(result)
			if err != nil || !result.Complete || !strings.Contains(string(data), `"threads":[]`) || !strings.Contains(string(data), `"reviews":[]`) {
				t.Fatalf("empty result=%s err=%v", data, err)
			}
		})
	}
}
