//nolint:wsl_v5 // GraphQL helpers keep operation setup and validation together.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Client calls Linear's GraphQL API.
type Client struct {
	url   string
	token string
	http  *http.Client
}

// NewClient returns a Linear GraphQL client.
func NewClient(url, token string) *Client {
	return &Client{url: strings.TrimRight(url, "/"), token: token, http: &http.Client{}}
}

// CreateActivity emits an agent activity.
func (c *Client) CreateActivity(ctx context.Context, sessionID string, content ActivityContent, ephemeral bool, signal string, signalMetadata map[string]any) error {
	input := map[string]any{
		"agentSessionId": sessionID,
		"content":        content,
	}
	if ephemeral {
		input["ephemeral"] = true
	}
	if signal != "" {
		input["signal"] = signal
	}
	if len(signalMetadata) > 0 {
		input["signalMetadata"] = signalMetadata
	}

	var out struct {
		Data struct {
			AgentActivityCreate struct {
				Success bool `json:"success"`
			} `json:"agentActivityCreate"`
		} `json:"data"`
	}
	err := c.graphql(ctx, `mutation AgentActivityCreate($input: AgentActivityCreateInput!) { agentActivityCreate(input: $input) { success } }`, map[string]any{"input": input}, &out)
	if err != nil {
		return err
	}
	if !out.Data.AgentActivityCreate.Success {
		return errors.New("linear agentActivityCreate returned success=false")
	}

	return nil
}

// UpdatePlan replaces the Linear agent session plan.
func (c *Client) UpdatePlan(ctx context.Context, sessionID string, plan []PlanStep) error {
	var out struct {
		Data struct {
			AgentSessionUpdate struct {
				Success bool `json:"success"`
			} `json:"agentSessionUpdate"`
		} `json:"data"`
	}
	err := c.graphql(ctx, `mutation AgentSessionUpdate($agentSessionId: String!, $data: AgentSessionUpdateInput!) { agentSessionUpdate(id: $agentSessionId, input: $data) { success } }`, map[string]any{
		"agentSessionId": sessionID,
		"data":           map[string]any{"plan": plan},
	}, &out)
	if err != nil {
		return err
	}
	if !out.Data.AgentSessionUpdate.Success {
		return errors.New("linear agentSessionUpdate returned success=false")
	}

	return nil
}

// StartedState returns the first started workflow state for a team.
func (c *Client) StartedState(ctx context.Context, teamID string) (string, error) {
	var out struct {
		Data struct {
			Team struct {
				States struct {
					Nodes []struct {
						ID       string  `json:"id"`
						Name     string  `json:"name"`
						Position float64 `json:"position"`
					} `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"data"`
	}
	err := c.graphql(ctx, `query TeamStartedStatuses($teamId: String!) { team(id: $teamId) { states(filter: { type: { eq: "started" } }) { nodes { id name position } } } }`, map[string]any{"teamId": teamID}, &out)
	if err != nil {
		return "", err
	}

	nodes := out.Data.Team.States.Nodes
	if len(nodes) == 0 {
		return "", errors.New("linear team has no started workflow state")
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Position < nodes[j].Position })
	return nodes[0].ID, nil
}

// UpdateIssue updates issue state/delegate fields.
func (c *Client) UpdateIssue(ctx context.Context, issueID, stateID, delegateID string) error {
	input := map[string]any{}
	if stateID != "" {
		input["stateId"] = stateID
	}
	if delegateID != "" {
		input["delegateId"] = delegateID
	}
	if len(input) == 0 {
		return nil
	}

	var out struct {
		Data struct {
			IssueUpdate struct {
				Success bool `json:"success"`
			} `json:"issueUpdate"`
		} `json:"data"`
	}
	err := c.graphql(ctx, `mutation IssueUpdate($issueId: String!, $input: IssueUpdateInput!) { issueUpdate(id: $issueId, input: $input) { success } }`, map[string]any{"issueId": issueID, "input": input}, &out)
	if err != nil {
		return err
	}
	if !out.Data.IssueUpdate.Success {
		return errors.New("linear issueUpdate returned success=false")
	}

	return nil
}

// IssueAttachments returns issue attachments for forwarding to the coding agent.
func (c *Client) IssueAttachments(ctx context.Context, issueID string) ([]Attachment, error) {
	var out struct {
		Data struct {
			Issue struct {
				Attachments struct {
					Nodes []Attachment `json:"nodes"`
				} `json:"attachments"`
			} `json:"issue"`
		} `json:"data"`
	}
	err := c.graphql(ctx, `query IssueAttachments($issueId: String!) { issue(id: $issueId) { attachments(first: 50, includeArchived: false, orderBy: createdAt) { nodes { id title subtitle url sourceType metadata } } } }`, map[string]any{"issueId": issueID}, &out)
	if err != nil {
		return nil, err
	}

	return out.Data.Issue.Attachments.Nodes, nil
}

func (c *Client) graphql(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("encode Linear GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Linear GraphQL request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call Linear GraphQL API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode Linear GraphQL response: %w", err)
	}

	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("linear GraphQL API returned %s", res.Status)
	}
	if len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		for _, err := range envelope.Errors {
			messages = append(messages, err.Message)
		}
		return fmt.Errorf("linear GraphQL error: %s", strings.Join(messages, "; "))
	}

	wrapped, err := json.Marshal(struct {
		Data json.RawMessage `json:"data"`
	}{Data: envelope.Data})
	if err != nil {
		return fmt.Errorf("wrap Linear GraphQL response: %w", err)
	}

	if err := json.Unmarshal(wrapped, out); err != nil {
		return fmt.Errorf("decode Linear GraphQL data: %w", err)
	}

	return nil
}
