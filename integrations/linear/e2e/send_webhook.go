// Package main sends a signed mock Linear AgentSessionEvent webhook.
//
//nolint:wsl_v5 // E2E helper builds one request payload in a compact main.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	url := flag.String("url", "", "Linear integration webhook URL")
	secret := flag.String("secret", "", "webhook signing secret")
	sessionID := flag.String("session", "as_e2e", "agent session ID")
	issueID := flag.String("issue", "issue_e2e", "issue ID")
	identifier := flag.String("identifier", "BAS-E2E", "issue identifier")
	flag.Parse()

	if *url == "" || *secret == "" {
		fmt.Fprintln(os.Stderr, "url and secret are required")
		os.Exit(1)
	}

	payload := map[string]any{
		"type":             "AgentSessionEvent",
		"action":           "created",
		"createdAt":        time.Now().UTC().Format(time.RFC3339Nano),
		"organizationId":   "org_e2e",
		"oauthClientId":    "oauth_e2e",
		"appUserId":        "app_e2e",
		"webhookId":        "wh_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"webhookTimestamp": time.Now().UnixMilli(),
		"promptContext":    "Please complete the Linear E2E task and return mock-opencode-response.",
		"agentSession": map[string]any{
			"id":             *sessionID,
			"status":         "pending",
			"organizationId": "org_e2e",
			"issueId":        *issueID,
			"issue": map[string]any{
				"id":          *issueID,
				"identifier":  *identifier,
				"title":       "Linear E2E",
				"description": "E2E verification issue",
				"url":         "https://linear.app/bastion/issue/" + *identifier,
				"teamId":      "team_e2e",
				"team": map[string]any{
					"id":   "team_e2e",
					"key":  "BAS",
					"name": "Bastion",
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	mac := hmac.New(sha256.New, []byte(*secret))
	_, _ = mac.Write(body)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, *url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Linear-Signature", hex.EncodeToString(mac.Sum(nil)))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	status := res.Status
	statusCode := res.StatusCode
	_ = res.Body.Close()
	if statusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "webhook returned %s\n", status)
		os.Exit(1)
	}
}
