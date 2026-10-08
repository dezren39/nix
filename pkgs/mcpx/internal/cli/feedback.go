package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/logstore"
)

// CmdFeedback submits human or automated feedback for an interaction trace.
func (a *App) CmdFeedback(ctx context.Context, args []string) error {
	fs := newFlagSet("feedback")
	scoreVal := fs.Float64("score", 1.0, "primary feedback score between 0.0 (bad) and 1.0 (good)")
	targetVal := fs.String("target", "", "feedback target: 'retrieval' (search relevance) or 'execution' (tool runtime success)")
	retrievalScoreVal := fs.Float64("retrieval-score", -1, "retrieval relevance score between 0.0 and 1.0")
	executionScoreVal := fs.Float64("execution-score", -1, "execution reliability score between 0.0 and 1.0")
	notesVal := fs.String("notes", "", "optional commentary, correction, or feedback reasoning")
	scoresJSON := fs.String("scores", "", "optional JSON map of multi-dimensional scores (e.g. '{\"relevance\":1.0,\"accuracy\":0.8}')")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		return fmt.Errorf("usage: mcpx feedback <trace-id> [--score <0.0-1.0>] [--target <retrieval|execution>] [--retrieval-score <0.0-1.0>] [--execution-score <0.0-1.0>] [--notes <text>] [--scores <json>]")
	}
	traceID := remaining[0]

	var scoresMap map[string]float64
	if *scoresJSON != "" {
		if err := json.Unmarshal([]byte(*scoresJSON), &scoresMap); err != nil {
			return fmt.Errorf("invalid --scores JSON: %w", err)
		}
	}

	c := a.Client()
	if err := c.EnsureDaemon(ctx); err != nil {
		return err
	}
	ep, err := c.Endpoint(ctx)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"traceId": traceID,
		"score":   *scoreVal,
		"scores":  scoresMap,
		"notes":   *notesVal,
	}
	if *targetVal != "" {
		payload["target"] = *targetVal
	}
	if *retrievalScoreVal >= 0 {
		payload["retrievalScore"] = *retrievalScoreVal
	}
	if *executionScoreVal >= 0 {
		payload["executionScore"] = *executionScoreVal
	}

	bodyData, _ := json.Marshal(payload)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(ep, "/")+"/v1/feedback", bytes.NewReader(bodyData))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("feedback submission failed (status %d): %s", resp.StatusCode, string(b))
	}

	var details []string
	if *targetVal != "" {
		details = append(details, fmt.Sprintf("target: %s", *targetVal))
	}
	if *retrievalScoreVal >= 0 {
		details = append(details, fmt.Sprintf("retrieval: %.2f", *retrievalScoreVal))
	}
	if *executionScoreVal >= 0 {
		details = append(details, fmt.Sprintf("execution: %.2f", *executionScoreVal))
	}
	detailStr := ""
	if len(details) > 0 {
		detailStr = fmt.Sprintf(" [%s]", strings.Join(details, ", "))
	}

	fmt.Printf("Feedback recorded successfully for trace %q (score: %.2f%s).\n", traceID, *scoreVal, detailStr)
	return nil
}

// CmdInteractions queries, searches, and inspects past interaction traces and feedback.
func (a *App) CmdInteractions(ctx context.Context, args []string) error {
	fs := newFlagSet("interactions")
	query := fs.String("q", "", "search text across inputs, outputs, and feedback notes")
	source := fs.String("source", "", "filter by source ('proxy', 'call', etc.)")
	session := fs.String("session", "", "filter by session ID")
	target := fs.String("target", "", "filter by target ('retrieval' or 'execution')")
	hasFeedback := fs.String("feedback", "", "filter feedback status: 'yes' (rated), 'no' (unrated), or empty")
	minScore := fs.Float64("min-score", -1, "minimum score filter")
	maxScore := fs.Float64("max-score", -1, "maximum score filter")
	sortBy := fs.String("sort-by", "", "sort order ('time', 'score', 'retrieval', 'execution')")
	limit := fs.Int("limit", 20, "maximum number of results")
	showID := fs.String("show", "", "inspect full details of a specific trace ID")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	c := a.Client()
	if err := c.EnsureDaemon(ctx); err != nil {
		return err
	}
	ep, err := c.Endpoint(ctx)
	if err != nil {
		return err
	}

	base := strings.TrimRight(ep, "/")

	// If --show <id> requested:
	if *showID != "" {
		httpReq, err := http.NewRequestWithContext(ctx, "GET", base+"/v1/interactions/"+url.PathEscape(*showID), nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("interaction not found: %s", string(b))
		}
		var rec logstore.InteractionRecord
		if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
			return err
		}

		fmt.Printf("Trace ID:     %s\n", rec.TraceID)
		fmt.Printf("Timestamp:    %s\n", rec.Timestamp.Format(time.RFC3339))
		fmt.Printf("Source:       %s\n", rec.Source)
		if rec.SessionID != "" {
			fmt.Printf("Session ID:   %s\n", rec.SessionID)
		}
		if len(rec.ToolsUsed) > 0 {
			fmt.Printf("Tools Used:   %s\n", strings.Join(rec.ToolsUsed, ", "))
		}
		if rec.Target != "" {
			fmt.Printf("Target:       %s\n", rec.Target)
		}
		if rec.RetrievalScore != nil {
			fmt.Printf("Retrieval:    %.2f\n", *rec.RetrievalScore)
		}
		if rec.ExecutionScore != nil {
			fmt.Printf("Execution:    %.2f\n", *rec.ExecutionScore)
		}
		if rec.Score != nil {
			fmt.Printf("Score:        %.2f\n", *rec.Score)
		} else {
			fmt.Printf("Score:        (unrated - no feedback yet)\n")
		}
		if rec.ScoresJSON != "" {
			fmt.Printf("Scores JSON:  %s\n", rec.ScoresJSON)
		}
		if rec.FeedbackNotes != "" {
			fmt.Printf("Notes:        %s\n", rec.FeedbackNotes)
		}
		fmt.Printf("\n--- Input ---\n%s\n", rec.Input)
		if rec.Output != "" {
			fmt.Printf("\n--- Output ---\n%s\n", rec.Output)
		}
		return nil
	}

	// Otherwise list/filter
	params := url.Values{}
	if *query != "" {
		params.Set("q", *query)
	}
	if *source != "" {
		params.Set("source", *source)
	}
	if *session != "" {
		params.Set("sessionId", *session)
	}
	if *target != "" {
		params.Set("target", *target)
	}
	if *hasFeedback == "yes" || *hasFeedback == "true" || *hasFeedback == "1" {
		params.Set("hasFeedback", "1")
	} else if *hasFeedback == "no" || *hasFeedback == "false" || *hasFeedback == "0" {
		params.Set("hasFeedback", "0")
	}
	if *minScore >= 0 {
		params.Set("minScore", strconv.FormatFloat(*minScore, 'f', 2, 64))
	}
	if *maxScore >= 0 {
		params.Set("maxScore", strconv.FormatFloat(*maxScore, 'f', 2, 64))
	}
	if *sortBy != "" {
		params.Set("sortBy", *sortBy)
	}
	if *limit > 0 {
		params.Set("limit", strconv.Itoa(*limit))
	}

	reqURL := base + "/v1/interactions"
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var list []logstore.InteractionRecord
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return err
	}

	if len(list) == 0 {
		fmt.Println("No matching interactions found.")
		return nil
	}

	fmt.Printf("%-14s  %-19s  %-7s  %-10s  %-8s  %-26s  %s\n", "TRACE ID", "TIMESTAMP", "SOURCE", "TARGET", "SCORE", "TOOLS", "INPUT PREVIEW")
	fmt.Println(strings.Repeat("-", 106))
	for _, it := range list {
		scoreStr := "-"
		if it.Score != nil {
			scoreStr = fmt.Sprintf("%.2f", *it.Score)
		}
		targetStr := "-"
		if it.Target != "" {
			targetStr = it.Target
		}
		toolsStr := strings.Join(it.ToolsUsed, ",")
		if len(toolsStr) > 24 {
			toolsStr = toolsStr[:21] + "..."
		}
		inputPreview := strings.ReplaceAll(it.Input, "\n", " ")
		if len(inputPreview) > 30 {
			inputPreview = inputPreview[:27] + "..."
		}
		tsStr := it.Timestamp.Format("2006-01-02 15:04:05")
		fmt.Printf("%-14s  %-19s  %-7s  %-10s  %-8s  %-26s  %s\n", it.TraceID, tsStr, it.Source, targetStr, scoreStr, toolsStr, inputPreview)
	}
	fmt.Printf("\nUse `mcpx interactions --show <trace-id>` to inspect details or `mcpx feedback <trace-id> [--target <retrieval|execution>] --score <0.0-1.0>` to rate.\n")
	return nil
}
