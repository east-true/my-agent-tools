package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestPRConversationIsOptInPaginatedAndCoveredByFinalHeadGuard(t *testing.T) {
	for _, scenario := range []string{"disabled", "complete", "page error", "duplicate", "head changed", "head unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			reads := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					reviewFixturePR(w, "abc", nil, reviewPageInfo{})
				case strings.HasSuffix(r.URL.Path, "/issues/7/comments"):
					reads++
					if r.URL.Query().Get("page") == "1" {
						w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/issues/7/comments?page=2>; rel="next"`, r.Host))
						fmt.Fprint(w, `[{"id":1,"user":null,"body":"리뷰 실행 제한\nexact","html_url":"https://github.com/owner/repo/pull/7#issuecomment-1","updated_at":"now"}]`)
					} else if scenario == "page error" {
						w.WriteHeader(403)
					} else if scenario == "duplicate" {
						fmt.Fprint(w, `[{"id":1,"body":"duplicate","html_url":"url"}]`)
					} else {
						fmt.Fprint(w, `[{"id":2,"body":"next","html_url":"url2"}]`)
					}
				case scenario == "head changed" && r.URL.Path == "/repos/owner/repo/pulls/7":
					fmt.Fprint(w, `{"head":{"sha":"changed"}}`)
				case scenario == "head unavailable" && r.URL.Path == "/repos/owner/repo/pulls/7":
					w.WriteHeader(403)
				default:
					if !reviewFixtureHistory(w, r) {
						t.Errorf("unexpected endpoint %s", r.URL)
						w.WriteHeader(500)
					}
				}
			})
			r, err := f.client.PullRequestReviews(context.Background(), "owner/repo", ReviewOptions{Number: 7, Conversation: scenario != "disabled"})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "disabled" {
				if reads != 0 || r.Conversation != nil || !r.Complete || !r.HeadVerified {
					t.Fatal(r, reads)
				}
				return
			}
			if reads != 2 || r.Conversation == nil || len(*r.Conversation) < 1 || (*r.Conversation)[0].Body != "리뷰 실행 제한\nexact" || (*r.Conversation)[0].Author != nil {
				t.Fatal(r, reads)
			}
			if r.Complete != (scenario == "complete") {
				t.Fatal("partial collection accepted", r)
			}
			if r.HeadVerified != (scenario != "head changed" && scenario != "head unavailable") {
				t.Fatal("head receipt is inaccurate", r)
			}
			if len(f.writes()) > 0 {
				t.Fatal("collection mutated GitHub")
			}
		})
	}
}
