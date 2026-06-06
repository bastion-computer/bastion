#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LINEAR_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$LINEAR_DIR/../.." && pwd)"
CORE_DIR="$REPO_DIR/core"

BASTION="$CORE_DIR/tmp/bastion"
LINEAR_BIN="$LINEAR_DIR/tmp/bastion-linear"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
RUN_ID="linear-e2e-$(date +%Y%m%d%H%M%S)-$$"
TAG="linear-e2e-$RUN_ID"
MOCK_PORT="${BASTION_LINEAR_E2E_MOCK_PORT:-3151}"
SERVICE_PORT="${BASTION_LINEAR_E2E_SERVICE_PORT:-3152}"
WEBHOOK_SECRET="linear-e2e-secret"
TEMPLATE_KEY="$RUN_ID-template"
ENV_ID=""
MOCK_PID=""
SERVICE_PID=""

log() { printf '[linear-e2e] %s\n' "$*"; }
fail() { log "FAIL: $*" >&2; exit 1; }
run_cli() { "$BASTION" --api-url "$API_URL" "$@"; }

cleanup() {
  local status=$?
  set +e
  if [ -n "$SERVICE_PID" ]; then kill "$SERVICE_PID" >/dev/null 2>&1 || true; fi
  if [ -n "$MOCK_PID" ]; then kill "$MOCK_PID" >/dev/null 2>&1 || true; fi
  if [ -n "$ENV_ID" ]; then run_cli env remove --id "$ENV_ID" >/dev/null 2>&1 || true; fi
  run_cli templates remove --key "$TEMPLATE_KEY" >/dev/null 2>&1 || true
  exit "$status"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then fail "$1 is required"; fi
}

precheck() {
  require_command curl
  require_command go
  require_command jq
  if [ ! -x "$BASTION" ]; then fail "CLI build not found at $BASTION"; fi
  if ! run_cli templates list >/dev/null 2>&1; then fail "Bastion API is not reachable on $API_URL"; fi
}

build_linear() {
  log "building Linear integration"
  (cd "$LINEAR_DIR" && go build -o ./tmp/bastion-linear ./cmd/bastion-linear)
}

fake_opencode_template() {
  jq -nc --arg tag "$TAG" '{
    actions: {
      init: [
        {run: "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get update\napt-get install -y --no-install-recommends curl python3\ncat >/usr/local/bin/opencode <<'\''OPENCODE'\''\n#!/usr/bin/env sh\nset -eu\nif [ \"${1:-}\" != serve ]; then echo mock-opencode; exit 0; fi\nshift\nport=4096\nwhile [ $# -gt 0 ]; do\n  case \"$1\" in --port) port=$2; shift 2;; *) shift;; esac\ndone\npython3 - \"$port\" <<'\''PY'\''\nimport json, sys\nfrom http.server import BaseHTTPRequestHandler, HTTPServer\nport = int(sys.argv[1])\nclass H(BaseHTTPRequestHandler):\n    def do_GET(self):\n        if self.path == \"/global/health\":\n            self.send_response(200); self.end_headers(); self.wfile.write(b\"{\\\"healthy\\\":true,\\\"version\\\":\\\"mock\\\"}\"); return\n        self.send_response(404); self.end_headers()\n    def do_POST(self):\n        length = int(self.headers.get(\"Content-Length\", \"0\"));\n        if length: self.rfile.read(length)\n        if self.path == \"/session\":\n            body = b\"{\\\"id\\\":\\\"mock-session\\\",\\\"title\\\":\\\"mock\\\"}\"\n        elif self.path.endswith(\"/message\"):\n            body = b\"{\\\"info\\\":{},\\\"parts\\\":[{\\\"type\\\":\\\"text\\\",\\\"text\\\":\\\"mock-opencode-response\\\"}]}\"\n        elif self.path.endswith(\"/abort\"):\n            body = b\"true\"\n        else:\n            self.send_response(404); self.end_headers(); return\n        self.send_response(200); self.send_header(\"Content-Type\", \"application/json\"); self.end_headers(); self.wfile.write(body)\nHTTPServer((\"127.0.0.1\", port), H).serve_forever()\nPY\nOPENCODE\nchmod +x /usr/local/bin/opencode"}
      ]
    }
  }'
}

create_environment() {
  log "creating E2E template and environment"
  run_cli templates create --key "$TEMPLATE_KEY" --config "$(fake_opencode_template)" >/dev/null
  ENV_ID="$(run_cli env create --template-key "$TEMPLATE_KEY" --tag "$TAG" | jq -r '.id')"
  [ -n "$ENV_ID" ] && [ "$ENV_ID" != null ] || fail "environment creation did not return an id"
}

start_mock_linear() {
  log "starting mock Linear API"
  (cd "$LINEAR_DIR" && go run ./cmd/mock-linear --addr "127.0.0.1:$MOCK_PORT" --webhook-secret "$WEBHOOK_SECRET") >"$LINEAR_DIR/tmp/mock-linear.log" 2>&1 &
  MOCK_PID=$!
  for _ in $(seq 1 60); do
    curl -fsS "http://127.0.0.1:$MOCK_PORT/health" >/dev/null 2>&1 && return
    sleep 1
  done
  fail "mock Linear API did not start"
}

start_linear_service() {
  log "starting Linear integration service"
  LINEAR_API_TOKEN=e2e-token \
  LINEAR_WEBHOOK_SECRET="$WEBHOOK_SECRET" \
  LINEAR_API_URL="http://127.0.0.1:$MOCK_PORT/graphql" \
  LINEAR_APP_USER_ID=app_e2e \
  BASTION_API_URL="$API_URL" \
  BASTION_LINEAR_ADDR="127.0.0.1:$SERVICE_PORT" \
  BASTION_LINEAR_DB="$LINEAR_DIR/tmp/$RUN_ID.sqlite.db" \
  BASTION_LINEAR_ENVIRONMENT_TAGS="$TAG" \
  "$LINEAR_BIN" >"$LINEAR_DIR/tmp/bastion-linear-e2e.log" 2>&1 &
  SERVICE_PID=$!
  for _ in $(seq 1 60); do
    curl -fsS "http://127.0.0.1:$SERVICE_PORT/health" >/dev/null 2>&1 && return
    sleep 1
  done
  fail "Linear integration did not start"
}

send_webhook() {
  log "sending signed Linear webhook"
  (cd "$LINEAR_DIR" && go run ./e2e/send_webhook.go --url "http://127.0.0.1:$SERVICE_PORT/webhooks/linear" --secret "$WEBHOOK_SECRET" --identifier BAS-E2E)
}

assert_response() {
  log "waiting for mock Linear response activity"
  for _ in $(seq 1 180); do
    if curl -fsS "http://127.0.0.1:$MOCK_PORT/activities" | jq -e '.activities[] | select(.type == "response" and (.body | contains("mock-opencode-response")))' >/dev/null; then
      log "Linear E2E passed"
      return
    fi
    sleep 1
  done
  fail "did not observe response activity; see $LINEAR_DIR/tmp/bastion-linear-e2e.log and $LINEAR_DIR/tmp/mock-linear.log"
}

main() {
  precheck
  trap cleanup EXIT
  build_linear
  mkdir -p "$LINEAR_DIR/tmp"
  start_mock_linear
  create_environment
  start_linear_service
  send_webhook
  assert_response
}

main "$@"
