// Package service coordinates Linear sessions across Bastion environments.
//
//nolint:wsl_v5,gocyclo,goconst // Worker orchestration intentionally keeps the first implementation in one service.
package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/integrations/linear/internal/bastion"
	"github.com/bastion-computer/bastion/integrations/linear/internal/database"
	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
	"github.com/bastion-computer/bastion/integrations/linear/internal/opencode"
)

const (
	jobPending  = "pending"
	jobRunning  = "running"
	jobComplete = "complete"
	jobError    = "error"

	jobCreated = "created"
	jobPrompt  = "prompt"
	jobStop    = "stop"

	sessionQueued  = "queued"
	sessionRunning = "running"
	sessionStopped = "stopped"
	sessionDone    = "complete"
	sessionError   = "error"

	assignmentActive = "active"
)

// LinearClient is the subset of Linear used by the worker.
type LinearClient interface {
	CreateActivity(context.Context, string, linear.ActivityContent, bool, string, map[string]any) error
	AgentSessionForIssue(context.Context, string, string) (linear.AgentSessionWebhook, bool, error)
	CreateAgentSessionOnIssue(context.Context, string) (linear.AgentSessionWebhook, error)
	UpdatePlan(context.Context, string, []linear.PlanStep) error
	StartedState(context.Context, string) (string, error)
	UpdateIssue(context.Context, string, string, string) error
	IssueAttachments(context.Context, string) ([]linear.Attachment, error)
}

// BastionClient is the subset of Bastion used by the worker.
type BastionClient interface {
	ListEnvironments(context.Context, []string) ([]bastion.Environment, error)
}

// OpenCodeClient is the subset of OpenCode used by the worker.
type OpenCodeClient interface {
	StartServer(context.Context, string) (int, error)
	StopServer(context.Context, string) error
	CreateSession(context.Context, string, string) (opencode.Session, error)
	SendMessage(context.Context, string, string, string, []linear.Attachment) (opencode.Response, error)
	Abort(context.Context, string, string) error
}

// Selector chooses which Bastion environments are eligible for Linear work.
type Selector struct {
	Tags        []string
	IDPatterns  []string
	KeyPatterns []string
}

// Config configures the worker service.
type Config struct {
	Selector       Selector
	AppUserID      string
	WorkerInterval time.Duration
}

// Service manages Linear jobs and environment assignments.
type Service struct {
	db       *database.Client
	linear   LinearClient
	bastion  BastionClient
	opencode OpenCodeClient
	cfg      Config
	logger   *slog.Logger
	notify   chan struct{}
}

// New returns a Linear worker service.
func New(db *database.Client, linearClient LinearClient, bastionClient BastionClient, opencodeClient OpenCodeClient, cfg Config, logger *slog.Logger) *Service {
	if cfg.WorkerInterval <= 0 {
		cfg.WorkerInterval = 5 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		db:       db,
		linear:   linearClient,
		bastion:  bastionClient,
		opencode: opencodeClient,
		cfg:      cfg,
		logger:   logger,
		notify:   make(chan struct{}, 1),
	}
}

// AcceptWebhook records a verified webhook and enqueues worker jobs.
func (s *Service) AcceptWebhook(ctx context.Context, payload linear.AgentSessionEventWebhookPayload, raw []byte) error {
	inserted, err := s.recordWebhook(ctx, payload.WebhookID, raw)
	if err != nil {
		return err
	}
	if !inserted {
		return nil
	}

	if payload.Type == "AppUserNotification" {
		if err := s.acceptNotificationWebhook(ctx, payload); err != nil {
			_ = s.deleteWebhook(ctx, payload.WebhookID)
			return err
		}

		return nil
	}

	created, err := s.upsertSession(ctx, payload)
	if err != nil {
		_ = s.deleteWebhook(ctx, payload.WebhookID)
		return err
	}

	switch payload.Action {
	case "created":
		if !created {
			break
		}
		if err := s.enqueueJob(ctx, payload.AgentSession.ID, jobCreated, payload.PromptContext); err != nil {
			_ = s.deleteWebhook(ctx, payload.WebhookID)
			return err
		}
	case "prompted":
		if payload.AgentActivity != nil && payload.AgentActivity.Signal == "stop" {
			if err := s.enqueueJob(ctx, payload.AgentSession.ID, jobStop, ""); err != nil {
				_ = s.deleteWebhook(ctx, payload.WebhookID)
				return err
			}
			break
		}

		body := linear.PromptBody(payload.AgentActivity)
		if body == "" {
			body = "The user provided a follow-up prompt in Linear. Continue from the latest session context."
		}
		if err := s.enqueueJob(ctx, payload.AgentSession.ID, jobPrompt, body); err != nil {
			_ = s.deleteWebhook(ctx, payload.WebhookID)
			return err
		}
	default:
		return fmt.Errorf("unsupported Linear agent session action %q", payload.Action)
	}

	s.wake()
	return nil
}

func (s *Service) acceptNotificationWebhook(ctx context.Context, payload linear.AgentSessionEventWebhookPayload) error {
	issueID := notificationIssueID(payload.Notification)
	if issueID == "" {
		return errors.New("linear app-user notification missing issue")
	}

	switch payload.Action {
	case "issueAssignedToYou":
		agentSession, ok, err := s.linear.AgentSessionForIssue(ctx, issueID, s.cfg.AppUserID)
		if err != nil {
			s.logger.WarnContext(ctx, "could not find existing Linear agent session", slog.String("error", err.Error()), slog.String("issue_id", issueID))
		}
		if !ok {
			agentSession, err = s.linear.CreateAgentSessionOnIssue(ctx, issueID)
			if err != nil {
				return fmt.Errorf("create Linear agent session for assigned issue: %w", err)
			}
		}
		if agentSession.IssueID == "" {
			agentSession.IssueID = issueID
		}
		if agentSession.Issue == nil && payload.Notification != nil {
			agentSession.Issue = payload.Notification.Issue
		}

		created, err := s.upsertSession(ctx, linear.AgentSessionEventWebhookPayload{
			Action:        "created",
			PromptContext: notificationPrompt(payload.Notification),
			AgentSession:  agentSession,
		})
		if err != nil {
			return err
		}
		if created {
			if err := s.enqueueJob(ctx, agentSession.ID, jobCreated, notificationPrompt(payload.Notification)); err != nil {
				return err
			}
		}
	case "issueUnassignedFromYou":
		session, ok, err := s.activeSessionByIssue(ctx, issueID)
		if err != nil || !ok {
			return err
		}
		if err := s.enqueueJob(ctx, session.AgentSessionID, jobStop, ""); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported Linear app-user notification action %q", payload.Action)
	}

	s.wake()
	return nil
}

// Start runs the worker loop until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	go s.worker(ctx)
}

func (s *Service) worker(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WorkerInterval)
	defer ticker.Stop()

	for {
		if err := s.processNext(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.ErrorContext(ctx, "linear worker failed", slog.String("error", err.Error()))
		}

		select {
		case <-ctx.Done():
			return
		case <-s.notify:
		case <-ticker.C:
		}
	}
}

func (s *Service) processNext(ctx context.Context) error {
	job, ok, err := s.claimJob(ctx)
	if err != nil || !ok {
		return err
	}

	if err := s.processJob(ctx, job); err != nil {
		if recordErr := s.failJob(ctx, job.ID, err); recordErr != nil {
			err = errors.Join(err, recordErr)
		}
		_ = s.recordSessionError(ctx, job.AgentSessionID, err.Error())
		_ = s.emit(ctx, job.AgentSessionID, "error", "Bastion Linear integration failed: "+err.Error(), false)

		return err
	}

	return s.completeJob(ctx, job.ID)
}

func (s *Service) processJob(ctx context.Context, job job) error {
	session, err := s.session(ctx, job.AgentSessionID)
	if err != nil {
		return err
	}

	switch job.Kind {
	case jobCreated:
		return s.processRun(ctx, session, job.Body)
	case jobPrompt:
		return s.processRun(ctx, session, job.Body)
	case jobStop:
		return s.processStop(ctx, session)
	default:
		return fmt.Errorf("unsupported job kind %q", job.Kind)
	}
}

func (s *Service) processRun(ctx context.Context, session session, prompt string) error {
	if prompt == "" {
		prompt = session.prompt()
	}

	if err := s.emit(ctx, session.AgentSessionID, "thought", "Bastion received the Linear session and is selecting an environment.", true); err != nil {
		return err
	}
	_ = s.linear.UpdatePlan(ctx, session.AgentSessionID, []linear.PlanStep{
		{Content: "Select an available Bastion environment", Status: "inProgress"},
		{Content: "Start OpenCode in the environment", Status: "pending"},
		{Content: "Run the Linear prompt", Status: "pending"},
	})

	if err := s.prepareIssue(ctx, session); err != nil {
		s.logger.WarnContext(ctx, "could not update Linear issue", slog.String("error", err.Error()), slog.String("issue_id", session.IssueID))
	}

	environmentID := session.EnvironmentID
	if environmentID == "" {
		selected, err := s.selectEnvironment(ctx, session.AgentSessionID)
		if err != nil {
			return err
		}
		environmentID = selected.ID
	}

	if err := s.emitAction(ctx, session.AgentSessionID, "Starting OpenCode", environmentID, ""); err != nil {
		return err
	}
	pid, err := s.opencode.StartServer(ctx, environmentID)
	if err != nil {
		return err
	}

	if session.OpenCodeSessionID == "" {
		created, err := s.opencode.CreateSession(ctx, environmentID, session.title())
		if err != nil {
			return err
		}
		session.OpenCodeSessionID = created.ID
	}

	if err := s.recordSessionRun(ctx, session.AgentSessionID, environmentID, session.OpenCodeSessionID, pid); err != nil {
		return err
	}

	attachments, err := s.issueAttachments(ctx, session.IssueID)
	if err != nil {
		s.logger.WarnContext(ctx, "could not fetch Linear issue attachments", slog.String("error", err.Error()), slog.String("issue_id", session.IssueID))
	}

	_ = s.linear.UpdatePlan(ctx, session.AgentSessionID, []linear.PlanStep{
		{Content: "Select an available Bastion environment", Status: "completed"},
		{Content: "Start OpenCode in the environment", Status: "completed"},
		{Content: "Run the Linear prompt", Status: "inProgress"},
	})
	if err := s.emitAction(ctx, session.AgentSessionID, "Prompting OpenCode", session.IssueIdentifier, ""); err != nil {
		return err
	}

	response, err := s.opencode.SendMessage(ctx, environmentID, session.OpenCodeSessionID, prompt, attachments)
	if err != nil {
		return err
	}

	if err := s.emit(ctx, session.AgentSessionID, "response", response.Text, false); err != nil {
		return err
	}
	_ = s.linear.UpdatePlan(ctx, session.AgentSessionID, []linear.PlanStep{
		{Content: "Select an available Bastion environment", Status: "completed"},
		{Content: "Start OpenCode in the environment", Status: "completed"},
		{Content: "Run the Linear prompt", Status: "completed"},
	})

	if err := s.opencode.StopServer(ctx, environmentID); err != nil {
		s.logger.WarnContext(ctx, "could not stop OpenCode server", slog.String("error", err.Error()), slog.String("environment_id", environmentID))
	}

	if err := s.releaseEnvironment(ctx, environmentID); err != nil {
		return err
	}

	return s.completeSession(ctx, session.AgentSessionID)
}

func (s *Service) processStop(ctx context.Context, session session) error {
	if session.EnvironmentID != "" && session.OpenCodeSessionID != "" {
		if err := s.opencode.Abort(ctx, session.EnvironmentID, session.OpenCodeSessionID); err != nil {
			s.logger.WarnContext(ctx, "could not abort OpenCode session", slog.String("error", err.Error()), slog.String("agent_session_id", session.AgentSessionID))
		}
	}
	if session.EnvironmentID != "" {
		if err := s.opencode.StopServer(ctx, session.EnvironmentID); err != nil {
			s.logger.WarnContext(ctx, "could not stop OpenCode server", slog.String("error", err.Error()), slog.String("environment_id", session.EnvironmentID))
		}
		if err := s.releaseEnvironment(ctx, session.EnvironmentID); err != nil {
			return err
		}
	}

	if err := s.stopSession(ctx, session.AgentSessionID); err != nil {
		return err
	}

	return s.emit(ctx, session.AgentSessionID, "response", "Stopped work on this Linear session and released the Bastion environment.", false)
}

func (s *Service) prepareIssue(ctx context.Context, session session) error {
	if session.IssueID == "" {
		return nil
	}

	stateID := ""
	if session.TeamID != "" {
		value, err := s.linear.StartedState(ctx, session.TeamID)
		if err != nil {
			return err
		}
		stateID = value
	}

	return s.linear.UpdateIssue(ctx, session.IssueID, stateID, s.cfg.AppUserID)
}

func (s *Service) issueAttachments(ctx context.Context, issueID string) ([]linear.Attachment, error) {
	if issueID == "" {
		return nil, nil
	}

	return s.linear.IssueAttachments(ctx, issueID)
}

func (s *Service) selectEnvironment(ctx context.Context, agentSessionID string) (bastion.Environment, error) {
	environments, err := s.bastion.ListEnvironments(ctx, s.cfg.Selector.Tags)
	if err != nil {
		return bastion.Environment{}, err
	}

	for _, environment := range environments {
		if !s.cfg.Selector.Match(environment) {
			continue
		}

		assigned, ok, err := s.tryAssignEnvironment(ctx, agentSessionID, environment.ID)
		if err != nil {
			return bastion.Environment{}, err
		}
		if ok {
			assigned.Tags = environment.Tags
			assigned.Key = environment.Key
			assigned.Status = environment.Status
			return assigned, nil
		}
	}

	return bastion.Environment{}, errors.New("no matching running Bastion environment is available")
}

// Match reports whether an environment is eligible for Linear work.
func (s Selector) Match(environment bastion.Environment) bool {
	if environment.Status != "running" {
		return false
	}

	if len(s.IDPatterns) > 0 && !matchAny(s.IDPatterns, environment.ID) {
		return false
	}

	if len(s.KeyPatterns) > 0 {
		if environment.Key == nil || !matchAny(s.KeyPatterns, *environment.Key) {
			return false
		}
	}

	return true
}

func matchAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, value)
		if err == nil && matched {
			return true
		}
		if err != nil && pattern == value {
			return true
		}
	}

	return false
}

func (s *Service) emit(ctx context.Context, sessionID, activityType, body string, ephemeral bool) error {
	return s.linear.CreateActivity(ctx, sessionID, linear.ActivityContent{"type": activityType, "body": body}, ephemeral, "", nil)
}

func (s *Service) emitAction(ctx context.Context, sessionID, action, parameter, result string) error {
	content := linear.ActivityContent{"type": "action", "action": action, "parameter": parameter}
	if result != "" {
		content["result"] = result
	}

	return s.linear.CreateActivity(ctx, sessionID, content, false, "", nil)
}

func (s *Service) wake() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

type session struct {
	AgentSessionID    string
	IssueID           string
	IssueIdentifier   string
	IssueTitle        string
	TeamID            string
	Status            string
	EnvironmentID     string
	OpenCodeSessionID string
	PromptContext     string
}

func (s session) title() string {
	if s.IssueIdentifier != "" && s.IssueTitle != "" {
		return s.IssueIdentifier + ": " + s.IssueTitle
	}
	if s.IssueTitle != "" {
		return s.IssueTitle
	}

	return "Linear session " + s.AgentSessionID
}

func (s session) prompt() string {
	if strings.TrimSpace(s.PromptContext) != "" {
		return s.PromptContext
	}

	var parts []string
	if s.IssueIdentifier != "" || s.IssueTitle != "" {
		parts = append(parts, "Linear issue: "+strings.TrimSpace(s.IssueIdentifier+" "+s.IssueTitle))
	}
	parts = append(parts, "Please work on this Linear issue using the repository in this Bastion environment.")

	return strings.Join(parts, "\n\n")
}

func notificationIssueID(notification *linear.NotificationWebhook) string {
	if notification == nil {
		return ""
	}
	if notification.Issue != nil && notification.Issue.ID != "" {
		return notification.Issue.ID
	}

	return notification.IssueID
}

func notificationPrompt(notification *linear.NotificationWebhook) string {
	if notification == nil || notification.Issue == nil {
		return ""
	}

	issue := notification.Issue
	var parts []string
	if issue.Identifier != "" || issue.Title != "" {
		parts = append(parts, "Linear issue: "+strings.TrimSpace(issue.Identifier+" "+issue.Title))
	}
	if strings.TrimSpace(issue.Description) != "" {
		parts = append(parts, "Description:\n"+strings.TrimSpace(issue.Description))
	}
	parts = append(parts, "Please work on this Linear issue using the repository in this Bastion environment.")

	return strings.Join(parts, "\n\n")
}

type job struct {
	ID             string
	AgentSessionID string
	Kind           string
	Body           string
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func randomID(prefix string) (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}

	return prefix + "_" + hex.EncodeToString(bytes[:]), nil
}

func (s *Service) recordWebhook(ctx context.Context, webhookID string, raw []byte) (bool, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO linear_webhook_events (webhook_id, received_at, payload) VALUES (?, ?, ?)`, webhookID, now(), string(raw))
	if err == nil {
		return true, nil
	}
	if database.IsConstraint(err) {
		return false, nil
	}

	return false, fmt.Errorf("record Linear webhook: %w", err)
}

func (s *Service) deleteWebhook(ctx context.Context, webhookID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM linear_webhook_events WHERE webhook_id = ?`, webhookID)
	return err
}

func (s *Service) upsertSession(ctx context.Context, payload linear.AgentSessionEventWebhookPayload) (bool, error) {
	var existing int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM linear_sessions WHERE agent_session_id = ?`, payload.AgentSession.ID).Scan(&existing)
	if err != nil {
		return false, fmt.Errorf("lookup Linear session: %w", err)
	}

	issueID := payload.AgentSession.IssueID
	issueIdentifier := ""
	issueTitle := ""
	teamID := ""
	if payload.AgentSession.Issue != nil {
		issueID = payload.AgentSession.Issue.ID
		issueIdentifier = payload.AgentSession.Issue.Identifier
		issueTitle = payload.AgentSession.Issue.Title
		teamID = payload.AgentSession.Issue.TeamID
		if teamID == "" && payload.AgentSession.Issue.Team != nil {
			teamID = payload.AgentSession.Issue.Team.ID
		}
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO linear_sessions (agent_session_id, issue_id, issue_identifier, issue_title, team_id, status, prompt_context, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_session_id) DO UPDATE SET
  issue_id = excluded.issue_id,
  issue_identifier = excluded.issue_identifier,
  issue_title = excluded.issue_title,
  team_id = excluded.team_id,
  prompt_context = CASE WHEN excluded.prompt_context != '' THEN excluded.prompt_context ELSE linear_sessions.prompt_context END,
  updated_at = excluded.updated_at`,
		payload.AgentSession.ID, issueID, issueIdentifier, issueTitle, teamID, sessionQueued, payload.PromptContext, now(), now())
	if err != nil {
		return false, fmt.Errorf("upsert Linear session: %w", err)
	}

	return existing == 0, nil
}

func (s *Service) enqueueJob(ctx context.Context, agentSessionID, kind, body string) error {
	id, err := randomID("job")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO linear_jobs (id, agent_session_id, kind, body, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, agentSessionID, kind, body, jobPending, now(), now())
	if err != nil {
		return fmt.Errorf("enqueue Linear job: %w", err)
	}

	return nil
}

func (s *Service) claimJob(ctx context.Context) (job, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return job{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var out job
	err = tx.QueryRowContext(ctx, `SELECT id, agent_session_id, kind, body FROM linear_jobs WHERE status = ? ORDER BY created_at LIMIT 1`, jobPending).Scan(&out.ID, &out.AgentSessionID, &out.Kind, &out.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return job{}, false, nil
	}
	if err != nil {
		return job{}, false, fmt.Errorf("select pending Linear job: %w", err)
	}

	result, err := tx.ExecContext(ctx, `UPDATE linear_jobs SET status = ?, attempts = attempts + 1, updated_at = ? WHERE id = ? AND status = ?`, jobRunning, now(), out.ID, jobPending)
	if err != nil {
		return job{}, false, fmt.Errorf("claim Linear job: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return job{}, false, err
	}
	if affected == 0 {
		return job{}, false, nil
	}

	if err := tx.Commit(); err != nil {
		return job{}, false, fmt.Errorf("commit Linear job claim: %w", err)
	}

	return out, true, nil
}

func (s *Service) session(ctx context.Context, agentSessionID string) (session, error) {
	var out session
	err := s.db.QueryRowContext(ctx, `SELECT agent_session_id, issue_id, issue_identifier, issue_title, team_id, status, environment_id, opencode_session_id, prompt_context FROM linear_sessions WHERE agent_session_id = ?`, agentSessionID).Scan(&out.AgentSessionID, &out.IssueID, &out.IssueIdentifier, &out.IssueTitle, &out.TeamID, &out.Status, &out.EnvironmentID, &out.OpenCodeSessionID, &out.PromptContext)
	if err != nil {
		return session{}, fmt.Errorf("load Linear session: %w", err)
	}

	return out, nil
}

func (s *Service) activeSessionByIssue(ctx context.Context, issueID string) (session, bool, error) {
	var out session
	err := s.db.QueryRowContext(ctx, `SELECT agent_session_id, issue_id, issue_identifier, issue_title, team_id, status, environment_id, opencode_session_id, prompt_context FROM linear_sessions WHERE issue_id = ? AND status NOT IN (?, ?) ORDER BY updated_at DESC LIMIT 1`, issueID, sessionDone, sessionStopped).Scan(&out.AgentSessionID, &out.IssueID, &out.IssueIdentifier, &out.IssueTitle, &out.TeamID, &out.Status, &out.EnvironmentID, &out.OpenCodeSessionID, &out.PromptContext)
	if errors.Is(err, sql.ErrNoRows) {
		return session{}, false, nil
	}
	if err != nil {
		return session{}, false, fmt.Errorf("load active Linear session by issue: %w", err)
	}

	return out, true, nil
}

func (s *Service) tryAssignEnvironment(ctx context.Context, agentSessionID, environmentID string) (bastion.Environment, bool, error) {
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT environment_id FROM linear_environment_assignments WHERE agent_session_id = ? AND status = ?`, agentSessionID, assignmentActive).Scan(&existing)
	if err == nil {
		return bastion.Environment{ID: existing}, true, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return bastion.Environment{}, false, fmt.Errorf("lookup existing environment assignment: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO linear_environment_assignments (environment_id, agent_session_id, status, assigned_at, updated_at) VALUES (?, ?, ?, ?, ?)`, environmentID, agentSessionID, assignmentActive, now(), now())
	if err == nil {
		return bastion.Environment{ID: environmentID}, true, nil
	}
	if database.IsConstraint(err) {
		return bastion.Environment{}, false, nil
	}

	return bastion.Environment{}, false, fmt.Errorf("assign environment: %w", err)
}

func (s *Service) recordSessionRun(ctx context.Context, agentSessionID, environmentID, opencodeSessionID string, pid int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE linear_sessions SET status = ?, environment_id = ?, opencode_session_id = ?, opencode_port = 4096, opencode_pid = ?, updated_at = ?, last_error = '' WHERE agent_session_id = ?`, sessionRunning, environmentID, opencodeSessionID, pid, now(), agentSessionID)
	if err != nil {
		return fmt.Errorf("record Linear session run: %w", err)
	}

	return nil
}

func (s *Service) completeSession(ctx context.Context, agentSessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE linear_sessions SET status = ?, environment_id = '', updated_at = ?, last_error = '' WHERE agent_session_id = ?`, sessionDone, now(), agentSessionID)
	return err
}

func (s *Service) stopSession(ctx context.Context, agentSessionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE linear_sessions SET status = ?, environment_id = '', stop_requested = 1, updated_at = ? WHERE agent_session_id = ?`, sessionStopped, now(), agentSessionID)
	return err
}

func (s *Service) recordSessionError(ctx context.Context, agentSessionID, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE linear_sessions SET status = ?, updated_at = ?, last_error = ? WHERE agent_session_id = ?`, sessionError, now(), message, agentSessionID)
	return err
}

func (s *Service) releaseEnvironment(ctx context.Context, environmentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM linear_environment_assignments WHERE environment_id = ?`, environmentID)
	return err
}

func (s *Service) completeJob(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE linear_jobs SET status = ?, updated_at = ?, last_error = '' WHERE id = ?`, jobComplete, now(), jobID)
	return err
}

func (s *Service) failJob(ctx context.Context, jobID string, jobErr error) error {
	_, err := s.db.ExecContext(ctx, `UPDATE linear_jobs SET status = ?, updated_at = ?, last_error = ? WHERE id = ?`, jobError, now(), jobErr.Error(), jobID)
	return err
}
