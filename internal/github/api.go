package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/east-true/my-agent-tools/internal/command"
	sdk "github.com/google/go-github/v92/github"
)

type API interface {
	Do(context.Context, string, string, any, any) (int, error)
}

// CursorAPI supports GET endpoints that paginate with an after cursor.
type CursorAPI interface {
	API
	GetCursorPage(context.Context, string, any) (string, error)
}

type SDK struct {
	Client *sdk.Client
}

func NewAPI(ctx context.Context, runner command.Runner) (API, error) {
	token := strings.TrimSpace(os.Getenv("GH_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	}
	if token == "" {
		// Existing gh credentials are optional. Only the token is read; API operations
		// use the Go SDK and never invoke gh issue/pr/api commands.
		out, err := runner.Run(ctx, nil, "gh", "auth", "token", "--hostname", "github.com")
		if err != nil {
			return nil, errors.New("set GH_TOKEN or GITHUB_TOKEN, or authenticate with optional gh auth login")
		}
		token = strings.TrimSpace(string(out))
	}
	if token == "" {
		return nil, errors.New("GitHub authentication token is empty")
	}
	client, err := sdk.NewClient(
		sdk.WithAuthToken(token),
		sdk.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
	)
	if err != nil {
		return nil, fmt.Errorf("initialize GitHub client: %w", err)
	}
	return SDK{Client: client}, nil
}

func (api SDK) Do(ctx context.Context, method, endpoint string, payload, target any) (int, error) {
	req, err := api.Client.NewRequest(ctx, method, endpoint, payload)
	if err != nil {
		return 0, err
	}
	mergeRequest := strings.Contains(req.URL.Path, "/pulls/") && (strings.HasSuffix(req.URL.Path, "/merge-async") || strings.Contains(req.URL.Path, "/merge-async/"))
	if mergeRequest {
		req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	}
	response, err := api.Client.Do(req, target)
	if err != nil {
		var accepted *sdk.AcceptedError
		if mergeRequest && errors.As(err, &accepted) {
			if decodeErr := json.Unmarshal(accepted.Raw, target); decodeErr != nil {
				return 0, fmt.Errorf("decode accepted merge request: %w", decodeErr)
			}
			return 0, nil
		}
		return 0, err
	}
	return response.NextPage, nil
}

func (api SDK) GetCursorPage(ctx context.Context, endpoint string, target any) (string, error) {
	req, err := api.Client.NewRequest(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	response, err := api.Client.Do(req, target)
	if err != nil {
		return "", err
	}
	return response.After, nil
}

func isNotFound(err error) bool {
	var response *sdk.ErrorResponse
	return errors.As(err, &response) && response.Response != nil && response.Response.StatusCode == http.StatusNotFound
}
