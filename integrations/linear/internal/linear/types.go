// Package linear contains Linear API and webhook helpers.
package linear

import "encoding/json"

// AgentSessionEventWebhookPayload is the Linear AgentSessionEvent webhook shape.
type AgentSessionEventWebhookPayload struct {
	Type             string                `json:"type"`
	Action           string                `json:"action"`
	CreatedAt        string                `json:"createdAt"`
	OrganizationID   string                `json:"organizationId"`
	OAuthClientID    string                `json:"oauthClientId"`
	AppUserID        string                `json:"appUserId"`
	WebhookID        string                `json:"webhookId"`
	WebhookTimestamp float64               `json:"webhookTimestamp"`
	PromptContext    string                `json:"promptContext"`
	AgentSession     AgentSessionWebhook   `json:"agentSession"`
	AgentActivity    *AgentActivityWebhook `json:"agentActivity"`
	Notification     *NotificationWebhook  `json:"notification"`
}

// AgentSessionWebhook is the nested agent session webhook object.
type AgentSessionWebhook struct {
	ID             string        `json:"id"`
	Status         string        `json:"status"`
	URL            string        `json:"url"`
	IssueID        string        `json:"issueId"`
	Issue          *IssueWebhook `json:"issue"`
	OrganizationID string        `json:"organizationId"`
}

// IssueWebhook is the issue object included in agent-session webhooks.
type IssueWebhook struct {
	ID          string       `json:"id"`
	Identifier  string       `json:"identifier"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	URL         string       `json:"url"`
	TeamID      string       `json:"teamId"`
	Team        *TeamWebhook `json:"team"`
}

// TeamWebhook is the Linear team object included in webhooks.
type TeamWebhook struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// AgentActivityWebhook is the activity object included in prompted webhooks.
type AgentActivityWebhook struct {
	ID             string          `json:"id"`
	AgentSessionID string          `json:"agentSessionId"`
	Content        json.RawMessage `json:"content"`
	Signal         string          `json:"signal"`
}

// ActivityContent is a Linear agent activity content payload.
type ActivityContent map[string]any

// PlanStep is one Linear agent plan entry.
type PlanStep struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

// Attachment is an issue attachment returned by Linear.
type Attachment struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	Subtitle   string         `json:"subtitle"`
	URL        string         `json:"url"`
	SourceType string         `json:"sourceType"`
	Metadata   map[string]any `json:"metadata"`
}

// NotificationWebhook is the subset of app-user notification webhooks used by this integration.
type NotificationWebhook struct {
	ID        string        `json:"id"`
	Type      string        `json:"type"`
	IssueID   string        `json:"issueId"`
	Issue     *IssueWebhook `json:"issue"`
	UserID    string        `json:"userId"`
	CreatedAt string        `json:"createdAt"`
	UpdatedAt string        `json:"updatedAt"`
}
