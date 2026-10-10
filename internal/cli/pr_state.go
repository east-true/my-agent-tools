package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/east-true/my-agent-tools/internal/github"
)

type inspectionState struct {
	Version      int                        `json:"version"`
	Repo         string                     `json:"repo"`
	Number       int                        `json:"number"`
	HeadSHA      string                     `json:"head_sha"`
	Items        map[string]json.RawMessage `json:"items"`
	Sections     string                     `json:"sections,omitempty"`
	Annotations  bool                       `json:"annotations"`
	MaxLogBytes  int64                      `json:"max_log_bytes"`
	Conversation bool                       `json:"conversation,omitempty"`
}

func lockInspectionState(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("inspection state is locked or unavailable; if a previous process stopped, inspect its .lock file: %w", err)
	}
	return func() { file.Close(); os.Remove(path + ".lock") }, nil
}

func readInspectionState(path, repo string, number int, sections ...string) (*inspectionState, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state inspectionState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid --state-file; preserve or reset it explicitly: %w", err)
	}
	if state.Version != 1 || !strings.EqualFold(state.Repo, repo) || state.Number != number || state.HeadSHA == "" || state.Items == nil {
		return nil, errors.New("--state-file version or repository/PR scope does not match")
	}
	expected := "checks,failures,reviews"
	if len(sections) > 0 && sections[0] != "" {
		expected = sections[0]
	}
	if state.Sections == "" {
		state.Sections = "checks,failures,reviews"
	}
	if state.Sections != expected {
		return nil, errors.New("--state-file sections differ; use a separate state file for each selection")
	}
	return &state, nil
}

func inspectionItems(result github.InspectResult) inspectionState {
	state := inspectionState{Version: 1, Repo: result.Repo, Number: result.Number, HeadSHA: result.PR.HeadSHA, Items: map[string]json.RawMessage{}, Sections: result.Sections}
	add := func(key string, value any) {
		data, _ := json.Marshal(value)
		state.Items[key] = data
	}
	add("pr", map[string]any{"status": result.Status, "pr": result.PR, "reasons": result.Reasons})
	for _, check := range result.Checks {
		add(fmt.Sprintf("check:%s:%d", check.Kind, check.ID), check)
	}
	for _, run := range result.Runs {
		add(fmt.Sprintf("run:%d", run.ID), run)
	}
	for _, failure := range result.Failures {
		add(fmt.Sprintf("failure:%d:%d", failure.Run.ID, failure.Run.Attempt), failure)
	}
	if result.Reviews != nil {
		if result.Reviews.Conversation != nil {
			state.Conversation = true
			for _, comment := range *result.Reviews.Conversation {
				add(fmt.Sprintf("conversation:%d", comment.ID), comment)
			}
		}
		for _, review := range result.Reviews.Reviews {
			add(fmt.Sprintf("review:%d", review.ID), review)
		}
		for _, thread := range result.Reviews.Threads {
			copy := thread
			copy.Comments = nil
			add("thread:"+thread.ID, copy)
			for _, comment := range thread.Comments {
				add("comment:"+comment.ID, map[string]any{"thread_id": thread.ID, "path": thread.Path, "comment": comment})
			}
		}
	}
	return state
}

func reuseInspectionEvidence(before *inspectionState, options *github.InspectOptions) {
	if before == nil {
		return
	}
	options.CachedHead = before.HeadSHA
	var metadata struct {
		PR github.PRState `json:"pr"`
	}
	if json.Unmarshal(before.Items["pr"], &metadata) == nil {
		options.CachedBase = metadata.PR.BaseSHA
	}
	options.CachedComments = map[string]github.ReviewComment{}
	for id, raw := range before.Items {
		if strings.HasPrefix(id, "failure:") && before.Annotations == options.Annotations && before.MaxLogBytes == options.MaxLogBytes {
			var failure github.CIFailureResult
			if json.Unmarshal(raw, &failure) == nil && failure.Complete {
				options.CachedFailures = append(options.CachedFailures, failure)
			}
		}
		if strings.HasPrefix(id, "comment:") {
			var record struct {
				Comment github.ReviewComment `json:"comment"`
			}
			if json.Unmarshal(raw, &record) == nil && record.Comment.ID != "" && record.Comment.UpdatedAt != "" {
				options.CachedComments[record.Comment.ID] = record.Comment
			}
		}
	}
}

func inspectionDelta(result github.InspectResult, before *inspectionState, full bool, statePath string) (any, inspectionState) {
	after := inspectionItems(result)
	if before == nil || before.HeadSHA != after.HeadSHA || !result.Complete || full {
		return result, after
	}
	added, changed := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	removed := []string{}
	for id, item := range after.Items {
		old, exists := before.Items[id]
		if !exists {
			added[id] = item
		} else if string(old) != string(item) {
			changed[id] = item
		}
	}
	for id := range before.Items {
		if _, exists := after.Items[id]; !exists {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	status := "changed"
	if len(added)+len(changed)+len(removed) == 0 {
		status = "unchanged"
	}
	output := map[string]any{"status": status, "repo": result.Repo, "number": result.Number, "head_sha": after.HeadSHA, "pr_status": result.Status, "complete": true, "state_file": statePath, "attention_required": result.Status == "blocked" || result.Status == "pending"}
	// Include current actionable evidence even when the change set is empty.
	// This lets a caller return after unrelated work without replaying history.
	if work := outstandingInspection(result); work != nil {
		output["outstanding"] = work
	}
	if status == "changed" {
		output["added"], output["changed"], output["removed"] = added, changed, removed
	}
	return output, after
}

type inspectionWork struct {
	Reviews      []github.PRReview            `json:"reviews,omitempty"`
	PR           github.PRState               `json:"pr"`
	Reasons      []github.MergeReason         `json:"reasons,omitempty"`
	Checks       []github.PRCheck             `json:"checks,omitempty"`
	Failures     []github.CIFailureResult     `json:"failures,omitempty"`
	Threads      []github.ReviewThread        `json:"threads,omitempty"`
	Conversation []github.ConversationComment `json:"conversation,omitempty"`
}

func outstandingInspection(result github.InspectResult) *inspectionWork {
	work := &inspectionWork{PR: result.PR, Reasons: result.Reasons, Failures: result.Failures}
	for _, check := range result.Checks {
		done := check.Status == "completed" || check.Status == "success"
		success := check.Conclusion == "success" || check.Conclusion == "neutral" || check.Conclusion == "skipped" || check.Kind == "status" && check.Status == "success"
		if !done || !success {
			work.Checks = append(work.Checks, check)
		}
	}
	if result.Reviews != nil {
		// 제출 이력은 원문 근거이며 현재 수정 의무로 재해석하지 않는다.
		work.Reviews = result.Reviews.Reviews
		if result.Reviews.Conversation != nil {
			work.Conversation = *result.Reviews.Conversation
		}
		for _, thread := range result.Reviews.Threads {
			if !thread.Resolved {
				work.Threads = append(work.Threads, thread)
			}
		}
	}
	if len(work.Reasons)+len(work.Checks)+len(work.Failures)+len(work.Threads)+len(work.Conversation)+len(work.Reviews) == 0 {
		return nil
	}
	return work
}
