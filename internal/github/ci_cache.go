package github

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// Callers obtain fresh authorized run metadata before this shared cache. Only
// complete, completed attempts are reusable, with identical collection options.
func (client Client) ciFailuresForRun(ctx context.Context, repo string, options CIFailureOptions, run CIRun) (CIFailureResult, error) {
	if err := options.Normalize(); err != nil {
		return CIFailureResult{}, err
	}
	config := client.cacheConfiguration()
	keyBytes, _ := json.Marshal(struct {
		Repo                     string
		ID                       int64
		Attempt                  int
		Head, Status, Conclusion string
		Annotations              bool
		MaxLogBytes              int64
	}{strings.ToLower(repo), run.ID, run.Attempt, run.HeadSHA, run.Status, run.Conclusion, options.Annotations, options.MaxLogBytes})
	key := string(keyBytes)
	var cached CIFailureResult
	if run.Status == "completed" && ctx.Err() == nil && config.load("ci", key, 30*24*time.Hour, &cached) && cached.Complete && cached.Status == "ok" && strings.EqualFold(cached.Repo, repo) && cached.Run.ID == run.ID && cached.Run.Attempt == run.Attempt && cached.Run.HeadSHA == run.HeadSHA && cached.Run.Status == run.Status && cached.Run.Conclusion == run.Conclusion {
		cached.Run = run
		return cached, nil
	}
	result, err := client.collectCIFailuresForRun(ctx, repo, options, run)
	if err == nil && result.Complete && run.Status == "completed" && ctx.Err() == nil {
		config.save("ci", key, 30*24*time.Hour, result)
	}
	return result, err
}
