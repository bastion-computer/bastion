---
title: Linear
description: Set up the Bastion Linear integration sidecar.
---

The Linear integration runs as a sidecar service on the Bastion host. It receives
Linear Agent Session webhooks, assigns matching Bastion environments to issues,
starts OpenCode inside the selected environment through the Bastion SSH API, and
reports progress back to Linear as Agent Activities.

The integration does not create or remove Bastion environments. Create and tag
the environments you want Linear to use ahead of time.

## Install

Install Bastion and the Linear integration service:

```sh
curl -fsSL https://bastion.computer/install.sh | bash -s -- --integration linear
```

The installer downloads a separate `bastion-linear` release artifact, installs
`bastion-linear.service`, and creates `/etc/default/bastion-linear` if it does
not already exist. Future installer runs preserve this file.

Restart after editing configuration:

```sh
sudo systemctl restart bastion-linear.service
```

## Linear App

Create a Linear OAuth application configured as an app actor. Enable Agent
Session Events webhooks and point the webhook URL at your Bastion Linear sidecar:

```text
https://<your-host>/webhooks/linear
```

Linear requires a public HTTPS URL for webhooks. The sidecar can still talk to
Bastion locally through `BASTION_API_URL`.

Request the scopes needed by your workflow, typically:

| Scope             | Purpose                                        |
| ----------------- | ---------------------------------------------- |
| `read`            | Read issues, sessions, teams, and attachments. |
| `write`           | Update issues and agent session metadata.      |
| `app:assignable`  | Let Linear delegate issues to the app user.    |
| `app:mentionable` | Let users mention the app in Linear.           |

Use a Linear app actor token or client credentials token. The first version of
the sidecar expects the token to be configured directly; it does not run an OAuth
callback flow.

## Configuration

Edit `/etc/default/bastion-linear`:

```sh
BASTION_API_URL="http://localhost:3148"
BASTION_LINEAR_ADDR="localhost:3150"
BASTION_LINEAR_DATA_DIR="/home/bastion/.bastion/linear"
BASTION_LINEAR_ENVIRONMENT_TAGS="linear"

LINEAR_API_URL="https://api.linear.app/graphql"
LINEAR_API_TOKEN="lin_api_or_oauth_token"
LINEAR_WEBHOOK_SECRET="webhook_signing_secret"
LINEAR_APP_USER_ID="linear_app_user_id"
```

Environment targeting supports exact tags and glob patterns:

| Setting                           | Description                                    |
| --------------------------------- | ---------------------------------------------- |
| `BASTION_LINEAR_ENVIRONMENT_TAGS` | Comma-separated tags. All tags must match.     |
| `BASTION_LINEAR_ENVIRONMENT_IDS`  | Comma-separated environment ID glob patterns.  |
| `BASTION_LINEAR_ENVIRONMENT_KEYS` | Comma-separated environment key glob patterns. |

OpenCode settings:

| Setting                             | Description                                     |
| ----------------------------------- | ----------------------------------------------- |
| `BASTION_LINEAR_OPENCODE_PORT`      | OpenCode server port inside the environment.    |
| `BASTION_LINEAR_OPENCODE_DIRECTORY` | Project directory used for OpenCode HTTP calls. |
| `BASTION_LINEAR_OPENCODE_AGENT`     | Optional OpenCode agent name.                   |
| `BASTION_LINEAR_OPENCODE_PROVIDER`  | Optional OpenCode provider ID.                  |
| `BASTION_LINEAR_OPENCODE_MODEL`     | Optional OpenCode model ID.                     |

## Environment Setup

Create environments with OpenCode installed and a tag selected by the sidecar:

```json
{
  "actions": {
    "init": [
      {
        "use": "setup_opencode",
        "with": {
          "auth": "{\"anthropic\":{\"type\":\"api\",\"key\":\"${{ env.ANTHROPIC_API_KEY }}\"}}",
          "config": "{\"model\":\"anthropic/claude-sonnet-4-20250514\",\"permission\":{\"edit\":\"allow\",\"bash\":\"allow\"}}"
        }
      }
    ]
  }
}
```

Then create an environment with the matching tag:

```sh
bastion env create --template-key linear-worker --tag linear
```

## Webhook Endpoint

The sidecar exposes:

```http
POST /webhooks/linear
GET /health
```

Webhook requests are verified with Linear's `Linear-Signature` HMAC-SHA256
header over the raw request body and rejected if the webhook timestamp is stale.

## Behavior

When Linear creates an Agent Session, the sidecar:

1. Stores the webhook and job in SQLite.
2. Emits an immediate `thought` activity.
3. Selects one matching running Bastion environment and records the assignment.
4. Moves the issue to the first started workflow state when available.
5. Starts `opencode serve` inside the environment through Bastion SSH.
6. Sends the Linear prompt and issue attachments to OpenCode.
7. Emits a final `response` or `error` activity.
8. Stops the OpenCode server and releases the environment assignment.

If Linear sends a `stop` signal, the sidecar aborts the OpenCode session,
stops the server, releases the environment, and confirms that it stopped.
