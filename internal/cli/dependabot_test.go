package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/east-true/my-agent-tools/internal/command"
	"github.com/east-true/my-agent-tools/internal/github"
)

type dependabotCLIAPI struct {
	calls []string
	err   error
	empty bool
}

const dependabotCLIAlert = `{"number":3,"state":"open","dependency":{"package":{"name":"parser","ecosystem":"go"},"manifest_path":"go.mod","scope":"runtime"},"security_advisory":{"ghsa_id":"GHSA-example","summary":"Parser vulnerability","description":"Detailed advisory text.","references":[{"url":"https://example.com/advisory"}]},"security_vulnerability":{"severity":"high","vulnerable_version_range":"< 1.0.1","first_patched_version":null},"html_url":"https://github.com/owner/repo/security/dependabot/3"}`

func (api *dependabotCLIAPI) Do(_ context.Context, method, endpoint string, payload, target any) (int, error) {
	if method != "GET" || payload != nil {
		return 0, errors.New("unexpected write")
	}
	api.calls = append(api.calls, endpoint)
	if api.err != nil {
		return 0, api.err
	}
	return 0, json.Unmarshal([]byte(dependabotCLIAlert), target)
}

func (api *dependabotCLIAPI) GetCursorPage(_ context.Context, endpoint string, target any) (string, error) {
	api.calls = append(api.calls, endpoint)
	if api.err != nil {
		return "", api.err
	}
	data := "[" + dependabotCLIAlert + "]"
	if api.empty {
		data = "[]"
	}
	return "", json.Unmarshal([]byte(data), target)
}

func invokeDependabot(t *testing.T, api *dependabotCLIAPI, args ...string) (int, string, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	code := run(context.Background(), append([]string{"github", "dependabot"}, args...), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) { return api, nil })
	return code, out.String(), stderr.String()
}

func TestDependabotHelpAndInvalidInputNeedNoAuthentication(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 0}, {[]string{"--help"}, 0}, {[]string{"-h"}, 0},
		{[]string{"list", "--help"}, 0}, {[]string{"view", "-h"}, 0},
		{[]string{"dismiss"}, 2},
		{[]string{"view", "--number", "0", "--json"}, 2},
		{[]string{"view", "--number", "-1"}, 2},
		{[]string{"list", "--state", "all,open", "--json"}, 2},
		{[]string{"list", "--state", "unknown"}, 2},
		{[]string{"list", "--severity", "urgent", "--json"}, 2},
		{[]string{"list", "--severity", "high,"}, 2},
		{[]string{"list", "unexpected"}, 2},
		{[]string{"view", "--severity", "high"}, 2},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := run(context.Background(), append([]string{"github", "dependabot"}, tc.args...), nil, &out, &stderr, fakeRunner{}, func(context.Context, command.Runner) (github.API, error) {
				t.Fatal("help or invalid input attempted authentication")
				return nil, errors.New("unavailable")
			})
			if code != tc.code {
				t.Fatalf("code=%d want %d: %s %s", code, tc.code, out.String(), stderr.String())
			}
		})
	}
}

func TestDependabotListAndViewOutputs(t *testing.T) {
	for _, action := range []string{"list", "view"} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(action+"/json="+map[bool]string{true: "true", false: "false"}[jsonOutput], func(t *testing.T) {
				api := &dependabotCLIAPI{}
				args := []string{action, "--repo", "owner/repo"}
				if action == "view" {
					args = append(args, "--number", "3")
				}
				if jsonOutput {
					args = append(args, "--json")
				}
				code, out, stderr := invokeDependabot(t, api, args...)
				if code != 0 || stderr != "" || len(api.calls) != 1 || !strings.Contains(out, "parser") || !strings.Contains(out, "GHSA-example") {
					t.Fatalf("code=%d out=%s stderr=%s calls=%v", code, out, stderr, api.calls)
				}
				if (action == "view") != strings.Contains(out, "Detailed advisory text.") {
					t.Fatalf("incorrect detail visibility: %s", out)
				}
				if jsonOutput {
					var result map[string]json.RawMessage
					if err := json.Unmarshal([]byte(out), &result); err != nil || string(result["status"]) != `"ok"` || !strings.Contains(out, `"first_patched_version":null`) {
						t.Fatalf("invalid JSON result: %s %v", out, err)
					}
				} else if !strings.Contains(out, "first patched: not available") {
					t.Fatalf("missing no-patch text: %s", out)
				}
			})
		}
	}
}

func TestDependabotJSONEmptyAndAPIError(t *testing.T) {
	code, out, stderr := invokeDependabot(t, &dependabotCLIAPI{empty: true}, "list", "--repo", "owner/repo", "--json")
	if code != 0 || stderr != "" || !strings.Contains(out, `"alerts":[]`) || !strings.Contains(out, `"count":0`) {
		t.Fatalf("empty output: %d %s %s", code, out, stderr)
	}
	code, out, stderr = invokeDependabot(t, &dependabotCLIAPI{err: errors.New("permission denied")}, "list", "--repo", "owner/repo", "--json")
	if code != 1 || stderr != "" || !strings.Contains(out, `"status":"error"`) || strings.Contains(out, `"alerts"`) {
		t.Fatalf("error output: %d %s %s", code, out, stderr)
	}
}
