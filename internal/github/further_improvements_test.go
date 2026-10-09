package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRPreparationDoesNotQueryIssueTypes(t *testing.T) {
	types := 0
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/issue-types") {
			types++
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"denied"}`)
			return
		}
		if strings.Contains(r.URL.Path, "/compare/") {
			fmt.Fprint(w, `{"ahead_by":1}`)
			return
		}
		if !commonResponse(w, r) {
			t.Errorf("unexpected %s", r.URL)
			w.WriteHeader(500)
		}
	})
	_, err := f.client.Prepare(context.Background(), "owner/repo", "pr", Spec{Prefix: "fix", Title: "improve workflow", Body: "수정 내용", Head: "fix/workflow"}, DefaultPolicy())
	if err != nil || types != 0 {
		t.Fatalf("PR queried issue-only catalog: types=%d err=%v", types, err)
	}
	_, err = f.client.Context(context.Background(), "owner/repo")
	if err == nil || types != 1 {
		t.Fatal("full context no longer queries issue types")
	}
}

func TestReviewBodiesAreBatchedAcrossThreadsAndPages(t *testing.T) {
	for _, count := range []int{20, 120} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			bodyReads := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/graphql" {
					var payload struct {
						Query     string
						Variables map[string]any
					}
					_ = json.NewDecoder(r.Body).Decode(&payload)
					if strings.HasPrefix(payload.Query, "query ReviewCommentBodies") {
						bodyReads++
						ids := payload.Variables["ids"].([]any)
						if len(ids) > 100 {
							t.Fatal("body batch exceeds 100")
						}
						nodes := []ReviewComment{}
						for _, id := range ids {
							nodes = append(nodes, ReviewComment{ID: id.(string), Body: "fresh " + id.(string), UpdatedAt: "new"})
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"nodes": nodes}})
						return
					}
					start, end := 0, min(count, 100)
					pageInfo := reviewPageInfo{}
					if payload.Variables["cursor"] != nil {
						start, end = 100, count
					} else if count > 100 {
						pageInfo = reviewPageInfo{HasNextPage: true, EndCursor: "next"}
					}
					threads := []graphReviewThread{}
					for i := start; i < end; i++ {
						threads = append(threads, reviewFixtureThread(fmt.Sprintf("T%d", i), false, false, ReviewComment{ID: fmt.Sprintf("C%d", i), UpdatedAt: "new"}))
					}
					reviewFixturePR(w, "abc", threads, pageInfo)
					return
				}
				if strings.HasSuffix(r.URL.Path, "/reviews") {
					fmt.Fprint(w, `[]`)
					return
				}
				fmt.Fprint(w, `{"head":{"sha":"abc"}}`)
			})
			result, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7, CachedHead: "abc", CachedComments: map[string]ReviewComment{"old": {ID: "old", UpdatedAt: "old"}}})
			if err != nil || !result.Complete || bodyReads != (count+99)/100 || len(result.Threads) != count {
				t.Fatalf("reads=%d result=%+v err=%v", bodyReads, result, err)
			}
			for _, thread := range result.Threads {
				if thread.Comments[0].Body != "fresh "+thread.Comments[0].ID {
					t.Fatal("body assigned to another thread")
				}
			}
		})
	}
}

func TestCIFailureCacheIsSharedAcrossCommandsAndCollectionScopes(t *testing.T) {
	client, logs, _ := mergeDiagnosticsFixture(t, "matrix")
	client.cache = cacheConfig{Root: t.TempDir(), Scope: "credential-a"}
	first, err := client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
	if err != nil || !first.Complete || len(logs.downloads) != 2 {
		t.Fatal(err)
	}
	merged, err := client.MergePullRequest(context.Background(), "owner/repo", quickMergeOptions())
	if err != nil || len(merged.Failures) != 1 || !merged.Failures[0].Complete || len(logs.downloads) != 2 {
		t.Fatalf("merge downloaded cached CI evidence: %v %+v", logs.downloads, merged)
	}
	inspected, err := client.InspectPR(context.Background(), "owner/repo", InspectOptions{Number: 7, Sections: "failures", Annotations: true})
	if err != nil || !inspected.Complete || len(logs.downloads) != 2 {
		t.Fatalf("inspect downloaded cached evidence: %v %v", logs.downloads, err)
	}
	_, err = client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: false})
	if err != nil || len(logs.downloads) != 4 {
		t.Fatal("different collection settings reused cache")
	}
	client.cache.Scope = "credential-b"
	_, err = client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
	if err != nil || len(logs.downloads) != 6 {
		t.Fatal("another credential reused CI cache")
	}
}

func TestCIFailureCacheRejectsPartialAndCorruptEvidence(t *testing.T) {
	client, logs, _ := mergeDiagnosticsFixture(t, "truncated")
	client.cache = cacheConfig{Root: t.TempDir(), Scope: "credential"}
	for range 2 {
		result, err := client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
		if err != nil || result.Complete {
			t.Fatal("partial evidence was accepted")
		}
	}
	if len(logs.downloads) != 4 {
		t.Fatal("partial evidence was reused")
	}
	logs.truncated = false
	_, err := client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(client.cache.Root, "*", "ci", "cache-*.json"))
	if len(files) != 1 {
		t.Fatalf("cache files=%v", files)
	}
	if err := os.WriteFile(files[0], []byte(`{"version":1,"value":{"complete":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := client.CIFailures(context.Background(), "owner/repo", CIFailureOptions{RunID: 42, Annotations: true})
	if err != nil || !result.Complete || len(logs.downloads) != 8 {
		t.Fatal("corrupt evidence was reused")
	}
}
