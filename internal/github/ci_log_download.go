package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (api SDK) CIJobLog(ctx context.Context, repo string, jobID, maxBytes int64) (string, bool, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || maxBytes <= 0 {
		return "", false, errors.New("invalid job log download parameters")
	}
	u, _, err := api.Client.Actions.GetWorkflowJobLogs(ctx, owner, name, jobID, 0)
	if err != nil {
		return "", false, err
	}
	baseURL, baseErr := url.Parse(api.Client.BaseURL())
	allowed := func(location *url.URL) bool {
		return location != nil && location.Host != "" && baseErr == nil && (location.Scheme == "https" || (location.Scheme == "http" && baseURL.Scheme == "http" && location.Host == baseURL.Host))
	}
	if !allowed(u) {
		return "", false, errors.New("GitHub returned an invalid job log download URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", false, errors.New("invalid job log download URL")
	}
	// Signed storage URLs must be fetched without the GitHub auth transport.
	caller := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !allowed(req.URL) || len(via) >= 5 {
			return errors.New("invalid job log redirect")
		}
		return nil
	}}
	response, err := caller.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		return "", false, errors.New("job log download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("job log download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return "", false, errors.New("read job log download failed")
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	return string(data), truncated, nil
}
