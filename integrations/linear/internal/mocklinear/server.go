// Package mocklinear provides a Linear GraphQL mock for tests and E2E.
//
//nolint:wsl_v5 // Mock GraphQL handlers keep schema branches compact.
package mocklinear

import (
	"encoding/json"
	"maps"
	"net/http"
	"strings"
	"sync"

	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
)

// Server is a mock Linear GraphQL API based on the agent-session schema shapes used by the integration.
type Server struct {
	secret string

	mu          sync.Mutex
	Activities  []linear.ActivityContent
	Plans       [][]linear.PlanStep
	IssueUpdate []IssueUpdate
	Attachments []linear.Attachment
}

// IssueUpdate records an issueUpdate mutation.
type IssueUpdate struct {
	IssueID    string
	StateID    string
	DelegateID string
}

// Snapshot is a copy of the mock server state.
type Snapshot struct {
	Activities  []linear.ActivityContent `json:"activities"`
	Plans       [][]linear.PlanStep      `json:"plans"`
	IssueUpdate []IssueUpdate            `json:"issueUpdates"`
}

// New returns a mock Linear API server handler.
func New(secret string) *Server {
	return &Server{secret: secret}
}

// ServeHTTP handles GraphQL requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.Contains(req.Query, "agentActivityCreate"):
		s.handleActivity(w, req.Variables)
	case strings.Contains(req.Query, "agentSessionUpdate"):
		s.handleSessionUpdate(w, req.Variables)
	case strings.Contains(req.Query, "TeamStartedStatuses") || strings.Contains(req.Query, "states(filter"):
		_, _ = w.Write([]byte(`{"data":{"team":{"states":{"nodes":[{"id":"state_started","name":"Started","position":1}]}}}}`))
	case strings.Contains(req.Query, "issueUpdate"):
		s.handleIssueUpdate(w, req.Variables)
	case strings.Contains(req.Query, "IssueAttachments"):
		s.handleAttachments(w)
	default:
		_, _ = w.Write([]byte(`{"data":{}}`))
	}
}

// SignedWebhook returns a signed webhook body and signature.
func (s *Server) SignedWebhook(payload linear.AgentSessionEventWebhookPayload) ([]byte, string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", err
	}

	return body, linear.SignWebhook(body, s.secret), nil
}

// Snapshot returns recorded GraphQL interactions.
func (s *Server) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	activities := append([]linear.ActivityContent(nil), s.Activities...)
	plans := append([][]linear.PlanStep(nil), s.Plans...)
	updates := append([]IssueUpdate(nil), s.IssueUpdate...)

	return Snapshot{Activities: activities, Plans: plans, IssueUpdate: updates}
}

func (s *Server) handleActivity(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	content, _ := input["content"].(map[string]any)
	activity := linear.ActivityContent{}
	maps.Copy(activity, content)

	s.mu.Lock()
	s.Activities = append(s.Activities, activity)
	s.mu.Unlock()

	_, _ = w.Write([]byte(`{"data":{"agentActivityCreate":{"success":true}}}`))
}

func (s *Server) handleSessionUpdate(w http.ResponseWriter, variables map[string]any) {
	data, _ := variables["data"].(map[string]any)
	planValue, _ := data["plan"].([]any)
	plan := make([]linear.PlanStep, 0, len(planValue))
	for _, entry := range planValue {
		item, _ := entry.(map[string]any)
		plan = append(plan, linear.PlanStep{Content: stringValue(item["content"]), Status: stringValue(item["status"])})
	}

	s.mu.Lock()
	s.Plans = append(s.Plans, plan)
	s.mu.Unlock()

	_, _ = w.Write([]byte(`{"data":{"agentSessionUpdate":{"success":true}}}`))
}

func (s *Server) handleIssueUpdate(w http.ResponseWriter, variables map[string]any) {
	input, _ := variables["input"].(map[string]any)
	update := IssueUpdate{IssueID: stringValue(variables["issueId"]), StateID: stringValue(input["stateId"]), DelegateID: stringValue(input["delegateId"])}

	s.mu.Lock()
	s.IssueUpdate = append(s.IssueUpdate, update)
	s.mu.Unlock()

	_, _ = w.Write([]byte(`{"data":{"issueUpdate":{"success":true}}}`))
}

func (s *Server) handleAttachments(w http.ResponseWriter) {
	s.mu.Lock()
	attachments := append([]linear.Attachment(nil), s.Attachments...)
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"issue": map[string]any{"attachments": map[string]any{"nodes": attachments}}}})
}

func stringValue(value any) string {
	if out, ok := value.(string); ok {
		return out
	}

	return ""
}
