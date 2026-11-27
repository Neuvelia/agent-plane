package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultResponsesCap bounds the author responses in the prompt, in bytes.
const defaultResponsesCap = 20_000

// issueComment is the part of a GitHub issue comment this tool reads.
type issueComment struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"body"`
	User      struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"user"`
}

// responseFilter selects the comments that count as author responses.
type responseFilter struct {
	// authorized holds the logins the workflow verified can merge.
	authorized map[string]bool
	// since is when the previous review was published; zero reads every comment.
	since time.Time
	// trigger is the comment that asked for this run; it is read even when it
	// predates the previous review.
	trigger int64
}

func (f responseFilter) accepts(comment issueComment) bool {
	if comment.User.Type == "Bot" || !f.authorized[comment.User.Login] {
		return false
	}
	return f.since.IsZero() || comment.CreatedAt.After(f.since) || comment.ID == f.trigger
}

func runResponses(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("responses", flag.ContinueOnError)
	commentsPath := flags.String("comments", "", "JSON from gh api --paginate --slurp .../issues/N/comments")
	loginsPath := flags.String("logins", "", "file with one authorized login per line")
	since := flags.String("since", "", "RFC 3339 time of the previous review; empty reads every comment")
	trigger := flags.String("trigger", "", "id of the comment that asked for this run")
	limit := flags.Int("cap", defaultResponsesCap, "byte cap for the responses")
	if err := flags.Parse(args); err != nil {
		return err
	}
	filter, err := buildFilter(*loginsPath, *since, *trigger)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(*commentsPath)
	if err != nil {
		return fmt.Errorf("read comments: %w", err)
	}
	comments, err := decodeComments(data)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, formatResponses(selectResponses(comments, filter), filter.trigger, *limit))
	return err
}

var (
	errInvalidTrigger = errors.New("trigger comment id is not a number")
	digits            = regexp.MustCompile(`^[0-9]+$`)
)

func buildFilter(loginsPath, since, trigger string) (responseFilter, error) {
	filter := responseFilter{authorized: map[string]bool{}}
	logins, err := os.ReadFile(loginsPath)
	if err != nil {
		return filter, fmt.Errorf("read logins: %w", err)
	}
	for _, login := range strings.Fields(string(logins)) {
		filter.authorized[login] = true
	}
	if since != "" {
		if filter.since, err = time.Parse(time.RFC3339, since); err != nil {
			return filter, fmt.Errorf("parse -since: %w", err)
		}
	}
	if trigger != "" {
		if !digits.MatchString(trigger) {
			return filter, fmt.Errorf("%w: %q", errInvalidTrigger, truncate(trigger, 30))
		}
		if filter.trigger, err = strconv.ParseInt(trigger, 10, 64); err != nil {
			return filter, fmt.Errorf("%w: %w", errInvalidTrigger, err)
		}
	}
	return filter, nil
}

// decodeComments accepts a single JSON array or the array of pages that
// `gh api --paginate --slurp` writes.
func decodeComments(data []byte) ([]issueComment, error) {
	var pages []json.RawMessage
	if err := json.Unmarshal(data, &pages); err != nil {
		return nil, fmt.Errorf("decode comments: %w", err)
	}
	var comments []issueComment
	for _, page := range pages {
		if bytes.HasPrefix(bytes.TrimSpace(page), []byte("[")) {
			var batch []issueComment
			if err := json.Unmarshal(page, &batch); err != nil {
				return nil, fmt.Errorf("decode comment page: %w", err)
			}
			comments = append(comments, batch...)
			continue
		}
		var single issueComment
		if err := json.Unmarshal(page, &single); err != nil {
			return nil, fmt.Errorf("decode comment: %w", err)
		}
		comments = append(comments, single)
	}
	return comments, nil
}

// selectResponses filters and orders newest first.
func selectResponses(comments []issueComment, filter responseFilter) []issueComment {
	var selected []issueComment
	for _, comment := range comments {
		if filter.accepts(comment) {
			selected = append(selected, comment)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].CreatedAt.Equal(selected[j].CreatedAt) {
			return selected[i].ID > selected[j].ID
		}
		return selected[i].CreatedAt.After(selected[j].CreatedAt)
	})
	return selected
}

// formatResponses puts the triggering comment first and whole, then the rest
// newest first while they fit the cap. A rebuttal is usually the newest
// comment, so the cap drops the oldest end. Each body is fenced, so a comment
// cannot forge the heading of another.
func formatResponses(responses []issueComment, trigger int64, limit int) string {
	var text strings.Builder
	omitted := 0
	for _, comment := range responses {
		if comment.ID == trigger {
			text.WriteString(formatResponse(comment))
		}
	}
	for _, comment := range responses {
		if comment.ID == trigger {
			continue
		}
		block := formatResponse(comment)
		if omitted > 0 || text.Len()+len(block) > limit {
			omitted++
			continue
		}
		text.WriteString(block)
	}
	if omitted > 0 {
		fmt.Fprintf(&text, "[%d older responses omitted at the size cap]\n", omitted)
	}
	return text.String()
}

func formatResponse(comment issueComment) string {
	return fmt.Sprintf("#### @%s at %s\n\n%s\n\n", comment.User.Login, comment.CreatedAt.UTC().Format(time.RFC3339), fenced(strings.TrimSpace(comment.Body), "markdown"))
}
