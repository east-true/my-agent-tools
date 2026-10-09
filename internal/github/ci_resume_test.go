package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func resumeCheckpoint() CIRerunCheckpoint {
	return CIRerunCheckpoint{Version: 1, Repo: "owner/repo", RunID: 42, PreviousAttempt: 1, ExpectedAttempt: 2, HeadSHA: "abc", Mode: "failed", RequestedAt: time.Now().UTC(), RequestAttempted: true, Accepted: true}
}

func TestResumeRerunNeverSendsAnotherRequest(t *testing.T) {
	for _, mode := range []string{"completed", "failed", "pending then complete", "newer attempt", "changed head", "unknown acceptance", "invalid checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			reads, posts := 0, 0
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					posts++
					t.Error("resume mutated GitHub")
					w.WriteHeader(500)
					return
				}
				if r.URL.Path == "/repos/owner/repo/actions/runs/42/attempts/2/jobs" {
					fmt.Fprint(w, `{"jobs":[{"id":11,"status":"completed","conclusion":"failure"}]}`)
					return
				}
				if r.URL.Path == "/repos/owner/repo/actions/runs/42/attempts/2" {
					rerunFixtureRun(w, 2, "completed", "failure")
					return
				}
				reads++
				if mode == "changed head" {
					fmt.Fprint(w, `{"id":42,"run_attempt":2,"head_sha":"foreign","status":"completed","conclusion":"success"}`)
					return
				}
				if mode == "newer attempt" {
					rerunFixtureRun(w, 3, "in_progress", "")
					return
				}
				if mode == "pending then complete" && reads == 1 {
					rerunFixtureRun(w, 1, "completed", "failure")
					return
				}
				conclusion := "success"
				if mode == "failed" {
					conclusion = "failure"
				}
				rerunFixtureRun(w, 2, "completed", conclusion)
			})
			f.client.API = rerunFixtureLogs{API: f.client.API}
			state := resumeCheckpoint()
			if mode == "unknown acceptance" {
				state.Accepted = false
			}
			if mode == "invalid checkpoint" {
				state.ExpectedAttempt = 3
			}
			options := quickRerunOptions()
			options.Resume = &state
			options.OnRequest = func(CIRerunCheckpoint) error { t.Fatal("resume invoked mutation checkpoint callback"); return nil }
			result, err := f.client.RerunCI(context.Background(), "owner/repo", options)
			if posts != 0 {
				t.Fatal("resume requested a new attempt")
			}
			if mode == "changed head" || mode == "invalid checkpoint" {
				if err == nil {
					t.Fatal("invalid identity accepted")
				}
				return
			}
			want := "completed"
			if mode == "failed" || mode == "newer attempt" {
				want = "failed"
			}
			if err != nil || result.Status != want || !result.Resumed || result.Run.Attempt != 2 || result.RerunRequested != (mode != "unknown acceptance") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if want == "failed" && (result.Failure == nil || result.Failure.Run.Attempt != 2) {
				t.Fatal("resume used another attempt's diagnostics")
			}
		})
	}
}

func TestRerunCheckpointsBeforeMutationAndAfterAcceptance(t *testing.T) {
	for _, failSave := range []bool{false, true} {
		t.Run(fmt.Sprint(failSave), func(t *testing.T) {
			posts := 0
			states := []CIRerunCheckpoint{}
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					posts++
					if len(states) != 1 || states[0].Accepted {
						t.Error("request was not checkpointed first")
					}
					w.WriteHeader(201)
					return
				}
				rerunFixtureRun(w, 1, "completed", "failure")
			})
			options := quickRerunOptions()
			options.Wait = false
			options.OnRequest = func(state CIRerunCheckpoint) error {
				if failSave {
					return errors.New("storage unavailable")
				}
				states = append(states, state)
				return nil
			}
			result, err := f.client.RerunCI(context.Background(), "owner/repo", options)
			if failSave {
				if err == nil || posts != 0 || result.RequestAttempted {
					t.Fatal("mutation preceded checkpoint success")
				}
				return
			}
			if err != nil || posts != 1 || len(states) != 2 || !states[1].Accepted || states[0].ExpectedAttempt != 2 {
				t.Fatalf("result=%+v states=%+v err=%v", result, states, err)
			}
		})
	}
}
