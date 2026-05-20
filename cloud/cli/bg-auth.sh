#!/usr/bin/env bash
# bg-auth.sh — interactive device authorization for Bright Guard.
#
# Initiates a device authorization, prompts the user to approve it in a
# browser, then writes the resulting bearer token to
# ~/.config/brightguard/credentials.
#
# Usage:
#   ./bg-auth.sh                # default label "$USER@$HOST"
#   BG_CLIENT_LABEL="ci-runner" ./bg-auth.sh
#   BG_CONTROL_PLANE=http://localhost:8080 ./bg-auth.sh
set -euo pipefail

CONTROL_PLANE="${BG_CONTROL_PLANE:-https://mcp-governance.infoblox.dev}"
CLIENT_LABEL="${BG_CLIENT_LABEL:-$(whoami)@$(hostname -s 2>/dev/null || hostname)}"
CRED_DIR="${HOME}/.config/brightguard"
CRED_FILE="${CRED_DIR}/credentials"

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required command: $1" >&2; exit 1; }
}
need curl

# jq is optional; fall back to grep/sed.
have_jq=0
if command -v jq >/dev/null 2>&1; then have_jq=1; fi

extract() {
  # extract <key> <json>
  local key="$1" body="$2"
  if [[ "$have_jq" == "1" ]]; then
    printf '%s' "$body" | jq -r --arg k "$key" '.[$k] // empty'
  else
    printf '%s' "$body" | sed -nE "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"([^\"]*)\".*/\1/p" | head -1
  fi
}

echo "==> initiating device authorization against $CONTROL_PLANE"
init_body=$(printf '{"clientLabel":"%s"}' "$CLIENT_LABEL")
init_resp=$(curl -sS -X POST "$CONTROL_PLANE/oauth/device" \
  -H 'Content-Type: application/json' \
  -d "$init_body")

device_code=$(extract deviceCode "$init_resp")
user_code=$(extract userCode "$init_resp")
verify_url=$(extract verificationUriComplete "$init_resp")
interval=$(extract interval "$init_resp")
[[ -z "$interval" ]] && interval=5

if [[ -z "$device_code" || -z "$user_code" || -z "$verify_url" ]]; then
  echo "could not parse initiate response: $init_resp" >&2
  exit 1
fi

echo
echo "  Open this URL in your browser to authorize:"
echo "    $verify_url"
echo
echo "  Or visit ${CONTROL_PLANE%/}/device and enter the code:"
echo "    $user_code"
echo

# Best-effort browser open.
if command -v open >/dev/null 2>&1; then
  open "$verify_url" 2>/dev/null || true
elif command -v xdg-open >/dev/null 2>&1; then
  xdg-open "$verify_url" 2>/dev/null || true
fi

echo "==> waiting for approval (Ctrl-C to abort)…"
poll_body=$(printf '{"deviceCode":"%s"}' "$device_code")
attempts=0
max_attempts=$((600 / interval))
while (( attempts < max_attempts )); do
  http_status=$(curl -sS -o /tmp/bg-auth-poll.$$ -w "%{http_code}" -X POST "$CONTROL_PLANE/oauth/device/poll" \
    -H 'Content-Type: application/json' \
    -d "$poll_body")
  case "$http_status" in
    200)
      poll_resp=$(cat /tmp/bg-auth-poll.$$)
      rm -f /tmp/bg-auth-poll.$$
      access_token=$(extract accessToken "$poll_resp")
      if [[ -z "$access_token" ]]; then
        echo "approval succeeded but token missing from response" >&2
        exit 1
      fi
      mkdir -p "$CRED_DIR"
      chmod 700 "$CRED_DIR"
      umask 077
      printf '%s' "$access_token" > "$CRED_FILE"
      chmod 600 "$CRED_FILE"
      echo
      echo "==> authorized. Token written to $CRED_FILE"
      echo
      echo "  Use it with curl:"
      echo "    export BG_TOKEN=\$(cat $CRED_FILE)"
      echo "    curl -H \"Authorization: Bearer \$BG_TOKEN\" $CONTROL_PLANE/api/me"
      exit 0
      ;;
    428)
      # pending — sleep and try again.
      ;;
    410)
      rm -f /tmp/bg-auth-poll.$$
      echo "authorization expired. Re-run this script." >&2
      exit 2
      ;;
    403)
      rm -f /tmp/bg-auth-poll.$$
      echo "authorization denied." >&2
      exit 3
      ;;
    *)
      rm -f /tmp/bg-auth-poll.$$
      echo "unexpected response (status $http_status):" >&2
      cat /tmp/bg-auth-poll.$$ 2>/dev/null || true
      exit 1
      ;;
  esac
  attempts=$((attempts + 1))
  sleep "$interval"
done

rm -f /tmp/bg-auth-poll.$$
echo "timed out waiting for approval." >&2
exit 2
