package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type ReviewOptions struct {
	Number int
	All    bool
}

func (options ReviewOptions) Validate() error {
	if options.Number <= 0 {
		return errors.New("--number must be a positive PR number")
	}
	return nil
}

type ReviewAuthor struct {
	Login string `json:"login"`
}

type PRReview struct {
	ID          int64         `json:"id"`
	Author      *ReviewAuthor `json:"user"`
	State       string        `json:"state"`
	Body        string        `json:"body"`
	CommitID    string        `json:"commit_id"`
	SubmittedAt string        `json:"submitted_at"`
	URL         string        `json:"html_url"`
}

type ReviewComment struct {
	ID        string        `json:"id"`
	Author    *ReviewAuthor `json:"author"`
	Body      string        `json:"body"`
	URL       string        `json:"url"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
	DiffHunk  string        `json:"diff_hunk"`
}

type ReviewThread struct {
	ID                string          `json:"id"`
	Path              string          `json:"path"`
	Line              *int            `json:"line"`
	StartLine         *int            `json:"start_line"`
	OriginalLine      *int            `json:"original_line"`
	OriginalStartLine *int            `json:"original_start_line"`
	DiffSide          string          `json:"diff_side"`
	StartDiffSide     string          `json:"start_diff_side"`
	Resolved          bool            `json:"is_resolved"`
	Outdated          bool            `json:"is_outdated"`
	Comments          []ReviewComment `json:"comments"`
}

type ReviewResult struct {
	Status         string         `json:"status"`
	Repo           string         `json:"repo"`
	Number         int            `json:"number"`
	URL            string         `json:"url"`
	HeadSHA        string         `json:"head_sha"`
	ReviewDecision string         `json:"review_decision"`
	Complete       bool           `json:"complete"`
	All            bool           `json:"all"`
	Reviews        []PRReview     `json:"reviews"`
	Threads        []ReviewThread `json:"threads"`
	Notes          []string       `json:"notes,omitempty"`
}

type reviewPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func (page reviewPageInfo) next(seen map[string]bool) (string, error) {
	if !page.HasNextPage {
		return "", nil
	}
	if page.EndCursor == "" || seen[page.EndCursor] {
		return "", errors.New("review pagination did not advance")
	}
	seen[page.EndCursor] = true
	return page.EndCursor, nil
}

type reviewComments struct {
	Nodes    []ReviewComment `json:"nodes"`
	PageInfo reviewPageInfo  `json:"pageInfo"`
}

type graphReviewThread struct {
	ReviewThread
	Connection *reviewComments `json:"comments"`
}

const reviewCommentFields = `id author{login} body url created_at:createdAt updated_at:updatedAt diff_hunk:diffHunk`
const reviewThreadFields = `id path line start_line:startLine original_line:originalLine original_start_line:originalStartLine diff_side:diffSide start_diff_side:startDiffSide is_resolved:isResolved is_outdated:isOutdated comments(first:100){nodes{` + reviewCommentFields + `}pageInfo{hasNextPage endCursor}}`

// PullRequestReviews reads submitted review history and unresolved threads by
// default. Bodies remain source text, not inferred tasks or resolutions.
func (client Client) PullRequestReviews(ctx context.Context, repo string, options ReviewOptions) (ReviewResult, error) {
	result := ReviewResult{Status: "ok", Repo: repo, Number: options.Number, Complete: true, All: options.All, Reviews: []PRReview{}, Threads: []ReviewThread{}}
	if err := options.Validate(); err != nil {
		return result, err
	}
	partial := func(err error) {
		result.Status, result.Complete = "partial", false
		result.Notes = append(result.Notes, err.Error())
	}
	owner, name, _ := strings.Cut(repo, "/")
	const query = `query PullRequestReviews($owner:String!,$name:String!,$number:Int!,$cursor:String){repository(owner:$owner,name:$name){pullRequest(number:$number){url head_sha:headRefOid review_decision:reviewDecision reviewThreads(first:100,after:$cursor){nodes{` + reviewThreadFields + `}pageInfo{hasNextPage endCursor}}}}}`
	var cursor any
	seen, threadIDs := map[string]bool{}, map[string]bool{}
	for {
		var response struct {
			graphErrors
			Data struct {
				Repository *struct {
					PR *struct {
						URL            string `json:"url"`
						HeadSHA        string `json:"head_sha"`
						ReviewDecision string `json:"review_decision"`
						Threads        *struct {
							Nodes    []graphReviewThread `json:"nodes"`
							PageInfo reviewPageInfo      `json:"pageInfo"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"owner": owner, "name": name, "number": options.Number, "cursor": cursor}}, &response)
		if err == nil {
			err = response.err()
		}
		if err == nil && (response.Data.Repository == nil || response.Data.Repository.PR == nil) {
			err = errors.New("GitHub returned no pull request")
		}
		if err == nil {
			pr := response.Data.Repository.PR
			if pr.URL == "" || pr.HeadSHA == "" || pr.Threads == nil {
				err = errors.New("GitHub returned incomplete PR review metadata")
			}
		}
		if err != nil {
			if result.HeadSHA == "" {
				return result, fmt.Errorf("read PR reviews: %w", err)
			}
			partial(fmt.Errorf("Remaining review threads unavailable: %w", err))
			break
		}
		pr := response.Data.Repository.PR
		if result.HeadSHA == "" {
			result.URL, result.HeadSHA, result.ReviewDecision = pr.URL, pr.HeadSHA, pr.ReviewDecision
		} else if pr.HeadSHA != result.HeadSHA {
			partial(errors.New("PR head changed during review collection; collect again for the new commit"))
			break
		}
		for _, thread := range pr.Threads.Nodes {
			if thread.Resolved && !options.All {
				continue
			}
			if thread.ID == "" || thread.Path == "" || threadIDs[thread.ID] {
				partial(errors.New("Review thread lacks identity/path or was repeated during pagination"))
				continue
			}
			threadIDs[thread.ID] = true
			comments, err := client.reviewThreadComments(ctx, thread.ID, thread.Connection)
			thread.ReviewThread.Comments = comments
			result.Threads = append(result.Threads, thread.ReviewThread)
			if err != nil {
				partial(fmt.Errorf("Thread %s comments incomplete: %w", thread.ID, err))
			}
		}
		next, err := pr.Threads.PageInfo.next(seen)
		if err != nil {
			partial(err)
			break
		}
		if next == "" {
			break
		}
		cursor = next
	}
	for page := 1; ; {
		var reviews []PRReview
		next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100&page=%d", repo, options.Number, page), nil, &reviews)
		if err != nil {
			partial(fmt.Errorf("Submitted review history incomplete: %w", err))
			break
		}
		for _, review := range reviews {
			if review.State != "PENDING" {
				result.Reviews = append(result.Reviews, review)
			}
		}
		if next == 0 {
			break
		}
		if next <= page {
			partial(errors.New("submitted review pagination did not advance"))
			break
		}
		page = next
	}
	var current struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := client.api(ctx, "GET", fmt.Sprintf("repos/%s/pulls/%d", repo, options.Number), nil, &current); err != nil {
		partial(fmt.Errorf("Unable to verify PR head after collection: %w", err))
	} else if current.Head.SHA != result.HeadSHA {
		partial(errors.New("PR head changed or is unavailable after review collection; collect again"))
	}
	return result, nil
}

func (client Client) reviewThreadComments(ctx context.Context, id string, connection *reviewComments) ([]ReviewComment, error) {
	comments := []ReviewComment{}
	seen := map[string]bool{}
	const query = `query ReviewThreadComments($id:ID!,$cursor:String!){node(id:$id){... on PullRequestReviewThread{comments(first:100,after:$cursor){nodes{` + reviewCommentFields + `}pageInfo{hasNextPage endCursor}}}}}`
	for {
		if connection == nil {
			return comments, errors.New("GitHub returned no review comment connection")
		}
		comments = append(comments, connection.Nodes...)
		next, err := connection.PageInfo.next(seen)
		if err != nil || next == "" {
			return comments, err
		}
		var response struct {
			graphErrors
			Data struct {
				Node *struct {
					Comments *reviewComments `json:"comments"`
				} `json:"node"`
			} `json:"data"`
		}
		if err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"id": id, "cursor": next}}, &response); err != nil {
			return comments, err
		}
		if err := response.err(); err != nil {
			return comments, err
		}
		if response.Data.Node == nil {
			return comments, errors.New("review thread disappeared during collection")
		}
		connection = response.Data.Node.Comments
	}
}
