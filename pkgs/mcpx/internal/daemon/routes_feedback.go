package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dezren39/mcpx/internal/logstore"
)

type feedbackSubmitReq struct {
	TraceID        string             `json:"traceId"`
	Score          float64            `json:"score"`
	Target         string             `json:"target,omitempty"`
	RetrievalScore *float64           `json:"retrievalScore,omitempty"`
	ExecutionScore *float64           `json:"executionScore,omitempty"`
	Scores         map[string]float64 `json:"scores,omitempty"`
	Notes          string             `json:"notes,omitempty"`
}

// handleFeedbackSubmit implements POST /v1/feedback.
func (s *Server) handleFeedbackSubmit(w http.ResponseWriter, r *http.Request) {
	fbStore := s.reg.FeedbackStore()
	if fbStore == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("feedback store not initialized"))
		return
	}

	var req feedbackSubmitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid json request: %w", err))
		return
	}
	if req.TraceID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("traceId is required"))
		return
	}

	params := logstore.FeedbackParams{
		TraceID:        req.TraceID,
		Score:          req.Score,
		Target:         req.Target,
		RetrievalScore: req.RetrievalScore,
		ExecutionScore: req.ExecutionScore,
		Scores:         req.Scores,
		Notes:          req.Notes,
	}

	if err := fbStore.ProvideFeedback(params); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("failed to save feedback: %w", err))
		return
	}

	resp := map[string]any{
		"status":  "ok",
		"traceId": req.TraceID,
		"score":   req.Score,
	}
	if req.Target != "" {
		resp["target"] = req.Target
	}
	if req.RetrievalScore != nil {
		resp["retrievalScore"] = *req.RetrievalScore
	}
	if req.ExecutionScore != nil {
		resp["executionScore"] = *req.ExecutionScore
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleInteractionsList implements GET /v1/interactions.
func (s *Server) handleInteractionsList(w http.ResponseWriter, r *http.Request) {
	fbStore := s.reg.FeedbackStore()
	if fbStore == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	q := r.URL.Query()
	filter := logstore.InteractionFilter{
		Query:     q.Get("q"),
		Source:    q.Get("source"),
		SessionID: q.Get("sessionId"),
		Target:    q.Get("target"),
		SortBy:    q.Get("sortBy"),
		SortDesc:  q.Get("desc") != "0",
	}

	if hasFbStr := q.Get("hasFeedback"); hasFbStr != "" {
		hasFb := hasFbStr == "1" || strings.ToLower(hasFbStr) == "true"
		filter.HasFeedback = &hasFb
	}
	if minStr := q.Get("minScore"); minStr != "" {
		if val, err := strconv.ParseFloat(minStr, 64); err == nil {
			filter.MinScore = &val
		}
	}
	if maxStr := q.Get("maxScore"); maxStr != "" {
		if val, err := strconv.ParseFloat(maxStr, 64); err == nil {
			filter.MaxScore = &val
		}
	}
	if limitStr := q.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			filter.Limit = l
		}
	}
	if offsetStr := q.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil {
			filter.Offset = o
		}
	}

	list, err := fbStore.List(filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []logstore.InteractionRecord{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleInteractionGet implements GET /v1/interactions/{id}.
func (s *Server) handleInteractionGet(w http.ResponseWriter, r *http.Request) {
	fbStore := s.reg.FeedbackStore()
	if fbStore == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("feedback store not initialized"))
		return
	}

	id := r.PathValue("id")
	if id == "" {
		id = r.URL.Query().Get("id")
	}
	if id == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("id is required"))
		return
	}

	rec, err := fbStore.Get(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("interaction not found: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, rec)
}
