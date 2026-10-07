package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const dependabotFixture = `{
  "number": 7, "state": "open",
  "dependency": {"package":{"name":"@example/parser","ecosystem":"npm"},"manifest_path":"package-lock.json","scope":"runtime","relationship":"direct"},
  "security_advisory": {"ghsa_id":"GHSA-example","cve_id":"CVE-2026-0001","summary":"Parser vulnerability","description":"Detailed advisory text.","severity":"high","references":[{"url":"https://example.com/advisory"}],"vulnerabilities":[{"first_patched_version":{"identifier":"9.0.0"}}]},
  "security_vulnerability": {"severity":"critical","vulnerable_version_range":"< 2.0.1","first_patched_version":{"identifier":"2.0.1"}},
  "html_url":"https://github.com/owner/repo/security/dependabot/7"
}`

func TestDependabotListCursorPaginationAndFilters(t *testing.T) {
	calls := 0
	const cursor = "next/+=="
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/repos/owner/repo/dependabot/alerts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
		}
		q := r.URL.Query()
		for key, want := range map[string]string{"per_page": "100", "sort": "created", "direction": "desc", "state": "open", "severity": "high,critical", "ecosystem": "npm", "package": "@example/parser"} {
			if q.Get(key) != want {
				t.Errorf("%s=%q want %q", key, q.Get(key), want)
			}
		}
		if calls == 1 {
			if q.Get("after") != "" {
				t.Error("first page has cursor")
			}
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/dependabot/alerts?after=next%%2F%%2B%%3D%%3D&per_page=100>; rel="next"`, r.Host))
		} else if calls == 2 {
			if q.Get("after") != cursor {
				t.Errorf("cursor lost: %q", q.Get("after"))
			}
		} else {
			t.Fatal("unexpected extra page")
		}
		fmt.Fprint(w, "["+dependabotFixture+"]")
	})
	alerts, err := f.client.ListDependabot(context.Background(), "owner/repo", DependabotOptions{Severity: "high, critical", Ecosystem: "npm", Package: "@example/parser"})
	if err != nil || calls != 2 || len(alerts) != 2 || len(f.writes()) != 0 {
		t.Fatalf("alerts=%+v calls=%d err=%v", alerts, calls, err)
	}
	alert := alerts[0]
	if alert.PatchedVersion == nil || *alert.PatchedVersion != "2.0.1" || alert.Severity != "critical" || alert.Description != "" || alert.Package != "@example/parser" || alert.Relationship != "direct" || alert.CVEID != "CVE-2026-0001" {
		t.Fatalf("incorrect alert projection: %+v", alert)
	}
}

func TestDependabotAllStatesAndEmptyResult(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("state") {
			t.Error("all must omit state filter")
		}
		fmt.Fprint(w, `[]`)
	})
	alerts, err := f.client.ListDependabot(context.Background(), "owner/repo", DependabotOptions{State: "all"})
	if err != nil || alerts == nil || len(alerts) != 0 {
		t.Fatalf("empty result=%v err=%v", alerts, err)
	}
}

func TestDependabotViewDetailAndNoPatch(t *testing.T) {
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/repos/owner/repo/dependabot/alerts/7" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, strings.Replace(dependabotFixture, `{"identifier":"2.0.1"}`, `null`, 1))
	})
	alert, err := f.client.ViewDependabot(context.Background(), "owner/repo", 7)
	if err != nil || alert.PatchedVersion != nil || alert.Description != "Detailed advisory text." || len(alert.References) != 1 || alert.References[0] != "https://example.com/advisory" || len(f.writes()) != 0 {
		t.Fatalf("alert=%+v err=%v", alert, err)
	}
	data, err := json.Marshal(alert)
	if err != nil || !strings.Contains(string(data), `"first_patched_version":null`) {
		t.Fatalf("missing explicit null patch: %s %v", data, err)
	}
}

func TestDependabotErrorsNeverLookLikeEmptySuccess(t *testing.T) {
	for _, status := range []int{403, 404, 422, 500} {
		for _, view := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/view=%t", status, view), func(t *testing.T) {
				f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
					fmt.Fprint(w, `{"message":"unavailable"}`)
				})
				var err error
				if view {
					_, err = f.client.ViewDependabot(context.Background(), "owner/repo", 7)
				} else {
					_, err = f.client.ListDependabot(context.Background(), "owner/repo", DependabotOptions{})
				}
				if err == nil || !strings.Contains(err.Error(), "read Dependabot alerts") {
					t.Fatalf("missing API error: %v", err)
				}
				if (status == 403 || status == 404) && !strings.Contains(err.Error(), "token permissions") {
					t.Fatalf("missing permission guidance: %v", err)
				}
			})
		}
	}
}

func TestDependabotPaginationRejectsRepeatedCursorAndLaterFailure(t *testing.T) {
	for _, laterFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(laterFailure), func(t *testing.T) {
			calls := 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 2 && laterFailure {
					w.WriteHeader(500)
					fmt.Fprint(w, `{"message":"failed second page"}`)
					return
				}
				w.Header().Set("Link", fmt.Sprintf(`<http://%s/repos/owner/repo/dependabot/alerts?after=same>; rel="next"`, r.Host))
				fmt.Fprint(w, "["+dependabotFixture+"]")
			})
			alerts, err := f.client.ListDependabot(context.Background(), "owner/repo", DependabotOptions{})
			if err == nil || alerts != nil || calls != 2 {
				t.Fatalf("partial list or endless pagination: %v err=%v calls=%d", alerts, err, calls)
			}
		})
	}
}
