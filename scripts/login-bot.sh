#!/usr/bin/env bash
# Login a Telegram bot account to the bridge via the provisioning API.
#
# Usage:
#   BOT_TOKEN=123456:ABC...  MATRIX_PASSWORD=secret  ./scripts/login-bot.sh
#   or pass args:
#   ./scripts/login-bot.sh <bot_token> <matrix_password>
#
# Optional env vars:
#   MATRIX_USER        (default: @telegramrelay:lkofoss.club)
#   MATRIX_HOMESERVER  (default: https://matrix.lkofoss.club)
#   BRIDGE_HOST        (default: auto-detected via kubectl on SSH_HOST)
#   BRIDGE_PORT        (default: 29317)
#   SSH_HOST           (default: root@lkofoss.club)

set -euo pipefail

BOT_TOKEN="${1:-${BOT_TOKEN:?'BOT_TOKEN required'}}"
MATRIX_PASSWORD="${2:-${MATRIX_PASSWORD:?'MATRIX_PASSWORD required'}}"
MATRIX_USER="${MATRIX_USER:-@telegramrelay:lkofoss.club}"
MATRIX_HOMESERVER="${MATRIX_HOMESERVER:-https://matrix.lkofoss.club}"
BRIDGE_PORT="${BRIDGE_PORT:-29317}"
SSH_HOST="${SSH_HOST:-root@lkofoss.club}"

# ── 1. Get Matrix access token ────────────────────────────────────────────────
echo "→ Logging into Matrix as $MATRIX_USER ..."
TOKEN_RESP=$(curl -sf -X POST "$MATRIX_HOMESERVER/_matrix/client/v3/login" \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"m.login.password\",\"identifier\":{\"type\":\"m.id.user\",\"user\":\"$MATRIX_USER\"},\"password\":\"$MATRIX_PASSWORD\"}")
ACCESS_TOKEN=$(echo "$TOKEN_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])")
echo "  ✓ got access token"

# ── 2. Resolve bridge address ─────────────────────────────────────────────────
if [[ -z "${BRIDGE_HOST:-}" ]]; then
    echo "→ Resolving bridge address via kubectl ..."
    BRIDGE_HOST=$(ssh "$SSH_HOST" "kubectl get svc telegram-bridge -n mautrix -o jsonpath='{.spec.clusterIP}'" 2>/dev/null)
    echo "  ✓ bridge at $BRIDGE_HOST:$BRIDGE_PORT"
fi

PROV="http://$BRIDGE_HOST:$BRIDGE_PORT/_matrix/provision/v3"
USER_PARAM="user_id=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$MATRIX_USER'))")"

remote_curl() {
    ssh "$SSH_HOST" "curl -sf $*" 2>/dev/null
}

# ── 3. Start bot login ────────────────────────────────────────────────────────
echo "→ Starting bot login ..."
START=$(remote_curl -X POST "'$PROV/login/start/bot?$USER_PARAM'" \
    -H "'Authorization: Bearer $ACCESS_TOKEN'" \
    -H "'Content-Type: application/json'" -d "'{}'")
PROCESS_ID=$(echo "$START" | python3 -c "import sys,json; print(json.load(sys.stdin)['login_id'])")
STEP_ID=$(echo "$START"    | python3 -c "import sys,json; print(json.load(sys.stdin)['step_id'])")
echo "  ✓ process: $PROCESS_ID"

# ── 4. Submit bot token ───────────────────────────────────────────────────────
echo "→ Submitting bot token ..."
RESULT=$(remote_curl -X POST "'$PROV/login/step/$PROCESS_ID/$STEP_ID/user_input?$USER_PARAM'" \
    -H "'Authorization: Bearer $ACCESS_TOKEN'" \
    -H "'Content-Type: application/json'" \
    -d "'{\"$STEP_ID\":\"$BOT_TOKEN\"}'")

TYPE=$(echo "$RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin).get('type',''))")
if [[ "$TYPE" == "complete" ]]; then
    LOGIN_ID=$(echo "$RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin)['complete']['user_login_id'])")
    NAME=$(echo "$RESULT"     | python3 -c "import sys,json; print(json.load(sys.stdin).get('instructions',''))")
    echo ""
    echo "✓ $NAME"
    echo "  Telegram login ID: $LOGIN_ID"
    echo ""
    echo "  Update default_relays in the bridge config to: [\"$LOGIN_ID\"]"
else
    echo "Unexpected response:"
    echo "$RESULT" | python3 -m json.tool
    exit 1
fi
