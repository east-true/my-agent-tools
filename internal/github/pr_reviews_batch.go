package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// REST의 숫자 ID와 같은 식별자를 쓰며 GraphQL Node ID로 대체하지 않는다.
const submittedReviewSelection = `submittedReviews:reviews(first:100){nodes{id:fullDatabaseId user:author{login} state body commit{oid} submitted_at:submittedAt html_url:url}pageInfo{hasNextPage endCursor}}`

type submittedReview struct {
	ID          json.RawMessage `json:"id"`
	Author      *ReviewAuthor   `json:"user"`
	State       string          `json:"state"`
	Body        string          `json:"body"`
	SubmittedAt string          `json:"submitted_at"`
	URL         string          `json:"html_url"`
	Commit      *struct {
		OID string `json:"oid"`
	} `json:"commit"`
}

type submittedReviewConnection struct {
	Nodes    []submittedReview `json:"nodes"`
	PageInfo *reviewPageInfo   `json:"pageInfo"`
}

func (client Client) collectSubmittedReviews(ctx context.Context, repo string, result *ReviewResult, page *submittedReviewConnection) error {
	owner, name, _ := strings.Cut(repo, "/")
	seen, ids := map[string]bool{}, map[int64]bool{}
	for {
		if page.Nodes == nil {
			return errors.New("submitted review page lacks nodes")
		}
		for _, node := range page.Nodes {
			if node.State == "PENDING" {
				continue
			}
			text := string(node.ID)
			if strings.HasPrefix(text, `"`) {
				if err := json.Unmarshal(node.ID, &text); err != nil {
					return err
				}
			}
			id, err := strconv.ParseInt(text, 10, 64)
			if err != nil || id <= 0 || ids[id] {
				return errors.New("submitted review has no unique 64-bit database ID")
			}
			ids[id] = true
			review := PRReview{ID: id, Author: node.Author, State: node.State, Body: node.Body, SubmittedAt: node.SubmittedAt, URL: node.URL}
			if node.Commit != nil {
				review.CommitID = node.Commit.OID
			}
			result.Reviews = append(result.Reviews, review)
		}
		if page.PageInfo == nil {
			return errors.New("submitted review page lacks pagination metadata")
		}
		cursor, err := page.PageInfo.next(seen)
		if err != nil || cursor == "" {
			return err
		}
		query := `query SubmittedReviews($owner:String!,$name:String!,$number:Int!,$cursor:String!){repository(owner:$owner,name:$name){pullRequest(number:$number){head_sha:headRefOid ` + strings.Replace(submittedReviewSelection, "reviews(first:100)", "reviews(first:100,after:$cursor)", 1) + `}}}`
		var response struct {
			graphErrors
			Data struct {
				Repository *struct {
					PR *struct {
						HeadSHA string                     `json:"head_sha"`
						Reviews *submittedReviewConnection `json:"submittedReviews"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		if err := client.api(ctx, "POST", "graphql", map[string]any{"query": query, "variables": map[string]any{"owner": owner, "name": name, "number": result.Number, "cursor": cursor}}, &response); err != nil {
			return err
		}
		if err := response.err(); err != nil {
			return err
		}
		if response.Data.Repository == nil || response.Data.Repository.PR == nil || response.Data.Repository.PR.Reviews == nil {
			return errors.New("GitHub returned no submitted review page")
		}
		pr := response.Data.Repository.PR
		if pr.HeadSHA != result.HeadSHA {
			return fmt.Errorf("PR head changed during submitted review collection")
		}
		page = pr.Reviews
	}
}
