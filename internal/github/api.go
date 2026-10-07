package github

import (
	"context"
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
	response, err := api.Client.Do(req, target)
	if err != nil {
		return 0, err
	}
	return response.NextPage, nil
}

func isNotFound(err error) bool {
	var response *sdk.ErrorResponse
	return errors.As(err, &response) && response.Response != nil && response.Response.StatusCode == http.StatusNotFound
}
