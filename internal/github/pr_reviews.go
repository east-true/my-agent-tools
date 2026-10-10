package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type ReviewOptions struct {
	Number         int
	All            bool
	Conversation   bool
	CachedHead     string
	CachedComments map[string]ReviewComment
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

// ConversationComment는 리뷰 스레드·제출 리뷰와 구분하는 일반 PR 댓글이다.
// 삭제된 작성자는 null로 보존한다.
type ConversationComment struct {
	ID        int64         `json:"id"`
	Author    *ReviewAuthor `json:"user"`
	Body      string        `json:"body"`
	URL       string        `json:"html_url"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
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
	Status         string                 `json:"status"`
	Repo           string                 `json:"repo"`
	Number         int                    `json:"number"`
	URL            string                 `json:"url"`
	HeadSHA        string                 `json:"head_sha"`
	HeadVerified   bool                   `json:"head_verified,omitempty"`
	ReviewDecision string                 `json:"review_decision"`
	Complete       bool                   `json:"complete"`
	All            bool                   `json:"all"`
	Reviews        []PRReview             `json:"reviews"`
	Threads        []ReviewThread         `json:"threads"`
	Conversation   *[]ConversationComment `json:"conversation,omitempty"`
	Notes          []string               `json:"notes,omitempty"`
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

// PullRequestReviews는 기본적으로 제출된 리뷰 이력과 미해결 스레드를 읽는다.
// 본문은 원문으로 보존하고 작업·해결 여부를 추론하지 않는다.
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
	sparse := options.CachedHead != "" && len(options.CachedComments) > 0
	fields := reviewCommentFields
	if sparse {
		fields = `id author{login} url created_at:createdAt updated_at:updatedAt`
	}
	threadFields := strings.Replace(reviewThreadFields, reviewCommentFields, fields, 1)
	query := `query PullRequestReviews($owner:String!,$name:String!,$number:Int!,$cursor:String){repository(owner:$owner,name:$name){pullRequest(number:$number){url head_sha:headRefOid review_decision:reviewDecision reviewThreads(first:100,after:$cursor){nodes{` + threadFields + `}pageInfo{hasNextPage endCursor}}}}}`
	firstQuery := strings.Replace(query, "reviewThreads(first:100", submittedReviewSelection+" reviewThreads(first:100", 1)
	if sparse {
		// 증분 스레드 조회의 원문 생략 계약은 유지한다.
		firstQuery = query
	}
	var submitted *submittedReviewConnection
	var cursor any
	seen, threadIDs := map[string]bool{}, map[string]bool{}
	for {
		var response struct {
			graphErrors
			Data struct {
				Repository *struct {
					PR *struct {
						URL            string                     `json:"url"`
						HeadSHA        string                     `json:"head_sha"`
						ReviewDecision string                     `json:"review_decision"`
						Submitted      *submittedReviewConnection `json:"submittedReviews"`
						Threads        *struct {
							Nodes    []graphReviewThread `json:"nodes"`
							PageInfo reviewPageInfo      `json:"pageInfo"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		pageQuery := query
		if cursor == nil {
			pageQuery = firstQuery
		}
		err := client.api(ctx, "POST", "graphql", map[string]any{"query": pageQuery, "variables": map[string]any{"owner": owner, "name": name, "number": options.Number, "cursor": cursor}}, &response)
		if err == nil {
			if graphErr := response.err(); graphErr != nil {
				// 일부 선택 필드의 오류로 이미 반환된 유효한 스레드 원문을 버리지 않는다.
				if response.Data.Repository != nil && response.Data.Repository.PR != nil && response.Data.Repository.PR.Threads != nil && response.Data.Repository.PR.HeadSHA != "" && response.Data.Repository.PR.URL != "" {
					partial(graphErr)
				} else {
					err = graphErr
				}
			}
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
			submitted = pr.Submitted
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
			comments, err := client.reviewThreadComments(ctx, thread.ID, thread.Connection, fields)
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
	if sparse {
		known := options.CachedComments
		if result.HeadSHA != options.CachedHead {
			known = nil
		}
		var comments []ReviewComment
		for _, thread := range result.Threads {
			comments = append(comments, thread.Comments...)
		}
		if err := client.hydrateReviewComments(ctx, comments, known); err != nil {
			partial(fmt.Errorf("Changed review bodies incomplete: %w", err))
		}
		start := 0
		for i := range result.Threads {
			end := start + len(result.Threads[i].Comments)
			copy(result.Threads[i].Comments, comments[start:end])
			start = end
		}
	}
	if submitted != nil {
		if err := client.collectSubmittedReviews(ctx, repo, &result, submitted); err != nil {
			partial(fmt.Errorf("Submitted review history incomplete: %w", err))
		}
	}
	// 선택 필드가 없는 응답은 REST로 보완하며, 정상 GraphQL 응답은 중복 조회하지 않는다.
	for page := 1; submitted == nil; {
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
	if options.Conversation {
		comments := []ConversationComment{}
		result.Conversation = &comments
		seen := map[int64]bool{}
		for page := 1; ; {
			var batch []ConversationComment
			next, err := client.API.Do(ctx, "GET", fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100&page=%d", repo, options.Number, page), nil, &batch)
			if err != nil {
				partial(fmt.Errorf("PR conversation incomplete: %w", err))
				break
			}
			for _, comment := range batch {
				if comment.ID <= 0 || comment.URL == "" || seen[comment.ID] {
					partial(errors.New("PR conversation comment lacks identity/URL or was repeated during pagination"))
					continue
				}
				seen[comment.ID] = true
				comments = append(comments, comment)
			}
			if next == 0 {
				break
			}
			if next <= page {
				partial(errors.New("PR conversation pagination did not advance"))
				break
			}
			page = next
		}
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
	} else {
		result.HeadVerified = true
	}
	return result, nil
}

func (client Client) reviewThreadComments(ctx context.Context, id string, connection *reviewComments, fields string) ([]ReviewComment, error) {
	comments := []ReviewComment{}
	seen := map[string]bool{}
	query := `query ReviewThreadComments($id:ID!,$cursor:String!){node(id:$id){... on PullRequestReviewThread{comments(first:100,after:$cursor){nodes{` + fields + `}pageInfo{hasNextPage endCursor}}}}}`
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

func (client Client) hydrateReviewComments(ctx context.Context, comments []ReviewComment, known map[string]ReviewComment) error {
	missing := []string{}
	positions := map[string]int{}
	for i, comment := range comments {
		if comment.ID == "" {
			return errors.New("review comment lacks an identity")
		}
		if _, exists := positions[comment.ID]; exists {
			return errors.New("review comment repeated during collection")
		}
		positions[comment.ID] = i
		if previous, ok := known[comment.ID]; ok && comment.UpdatedAt != "" && previous.UpdatedAt == comment.UpdatedAt {
			comments[i].Body, comments[i].DiffHunk = previous.Body, previous.DiffHunk
		} else {
			missing = append(missing, comment.ID)
		}
	}
	const query = `query ReviewCommentBodies($ids:[ID!]!){nodes(ids:$ids){... on PullRequestReviewComment{` + reviewCommentFields + `}}}`
	for start := 0; start < len(missing); start += 100 {
		ids := missing[start:min(start+100, len(missing))]
		var response struct {
			graphErrors
			Data struct {
				Nodes []*ReviewComment `json:"nodes"`
			} `json:"data"`
		}
		if err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"ids": ids}}, &response); err != nil {
			return err
		}
		if err := response.err(); err != nil {
			return err
		}
		if len(response.Data.Nodes) != len(ids) {
			return errors.New("review body response is incomplete")
		}
		for i, node := range response.Data.Nodes {
			if node == nil || node.ID != ids[i] {
				return errors.New("review comment disappeared or changed identity during collection")
			}
			comments[positions[node.ID]] = *node
		}
	}
	return nil
}
