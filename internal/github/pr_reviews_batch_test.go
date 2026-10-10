package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestBatchedSubmittedReviewsPreserveFieldsAndPaginateWithoutREST(t *testing.T) {
	for _, scenario := range []string{"complete", "page denied", "head changed", "duplicate", "missing page info", "invalid id"} {
		t.Run(scenario, func(t *testing.T) {
			graphCalls, headCalls := 0, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/pulls/7" {
					headCalls++
					fmt.Fprint(w, `{"head":{"sha":"abc"}}`)
					return
				}
				if r.URL.Path != "/graphql" {
					t.Errorf("unnecessary endpoint %s", r.URL)
					w.WriteHeader(500)
					return
				}
				graphCalls++
				var payload struct {
					Query string         `json:"query"`
					Vars  map[string]any `json:"variables"`
				}
				json.NewDecoder(r.Body).Decode(&payload)
				if graphCalls == 1 {
					if !strings.Contains(payload.Query, "id:fullDatabaseId") || !strings.Contains(payload.Query, "reviewThreads(first:100") {
						t.Fatal("first request did not batch compatible fields", payload.Query)
					}
					fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"url":"url","head_sha":"abc","review_decision":"CHANGES_REQUESTED","reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"submittedReviews":{"nodes":[{"id":"9007199254740993","user":null,"body":"한국어\r\nexact","state":"DISMISSED","commit":{"oid":"old"},"submitted_at":"then","html_url":"review-url"},{"id":null,"state":"PENDING"}],"pageInfo":{"hasNextPage":true,"endCursor":"r1"}}}}}}`)
					return
				}
				if payload.Vars["cursor"] != "r1" || !strings.HasPrefix(payload.Query, "query SubmittedReviews") {
					t.Fatal("wrong independent review cursor", payload)
				}
				if scenario == "page denied" {
					w.WriteHeader(403)
					return
				}
				id, head := "2", "abc"
				if scenario == "duplicate" {
					id = "9007199254740993"
				}
				if scenario == "invalid id" {
					id = "null"
				}
				if scenario == "head changed" {
					head = "changed"
				}
				page := `,"pageInfo":{"hasNextPage":false}`
				if scenario == "missing page info" {
					page = ""
				}
				fmt.Fprintf(w, `{"data":{"repository":{"pullRequest":{"head_sha":%q,"submittedReviews":{"nodes":[{"id":%s,"state":"APPROVED","body":"second"}]%s}}}}}`, head, id, page)
			})
			r, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7})
			if err != nil || graphCalls != 2 || headCalls != 1 || len(r.Reviews) < 1 || r.Reviews[0].ID != 9007199254740993 || r.Reviews[0].Body != "한국어\r\nexact" || r.Reviews[0].CommitID != "old" || r.Reviews[0].Author != nil {
				t.Fatal(r, err, graphCalls, headCalls)
			}
			if r.Complete != (scenario == "complete") || !r.HeadVerified {
				t.Fatal("history error or final guard lost", r)
			}
		})
	}
}

func TestBatchedEmptyHistoryNeedsOnlyCollectionAndFinalHead(t *testing.T) {
	calls := 0
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"url":"url","head_sha":"abc","reviewThreads":{"nodes":[],"pageInfo":{"hasNextPage":false}},"submittedReviews":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)
			return
		}
		if !reviewFixtureHistory(w, r) {
			t.Fatal(r.URL)
		}
	})
	r, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7})
	if err != nil || !r.Complete || !r.HeadVerified || calls != 2 || len(r.Reviews) != 0 {
		t.Fatal(r, err, calls)
	}
}

func TestPartialGraphQLHistoryErrorRetainsReturnedThreads(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"errors":[{"message":"history denied"}],"data":{"repository":{"pullRequest":{"url":"url","head_sha":"abc","reviewThreads":{"nodes":[{"id":"T1","path":"x.go","comments":{"nodes":[{"id":"C1","body":"keep original"}],"pageInfo":{"hasNextPage":false}}}],"pageInfo":{"hasNextPage":false}},"submittedReviews":null}}}}`)
			return
		}
		if !reviewFixtureHistory(w, r) {
			t.Fatal(r.URL)
		}
	})
	r, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7})
	if err != nil || r.Complete || !r.HeadVerified || len(r.Threads) != 1 || r.Threads[0].Comments[0].Body != "keep original" || len(r.Notes) == 0 {
		t.Fatal("available evidence lost or error hidden", r, err)
	}
}
