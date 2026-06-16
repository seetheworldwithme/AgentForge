#!/usr/bin/env bash
# smoke.sh — end-to-end smoke test for the agent-rust core service.
#
# Usage:
#   make run            # in one terminal
#   BASE_URL=http://127.0.0.1:<port> API_KEY=sk-... ./scripts/smoke.sh
#
# Optional: KB_FILE=path/to/a.txt uploads a doc and waits for ingest.
set -euo pipefail

: "${BASE_URL:?BASE_URL is required}"
: "${API_KEY:?API_KEY is required}"
CHAT_MODEL="${CHAT_MODEL:-gpt-4o-mini}"
EMBED_MODEL="${EMBED_MODEL:-text-embedding-3-small}"

echo "==> healthz"
curl -s "$BASE_URL/healthz"; echo

echo "==> create provider"
PROV=$(curl -s -X POST "$BASE_URL/api/providers" -H "Content-Type: application/json" \
  -d "{\"name\":\"smoke\",\"base_url\":\"https://api.openai.com/v1\",\"api_key\":\"$API_KEY\",\"chat_model\":\"$CHAT_MODEL\",\"embed_model\":\"$EMBED_MODEL\",\"is_default\":true}")
echo "$PROV"
PROV_ID=$(echo "$PROV" | python -c "import sys,json;print(json.load(sys.stdin)['id'])")

echo "==> create session"
SESS=$(curl -s -X POST "$BASE_URL/api/sessions" -H "Content-Type: application/json" \
  -d "{\"title\":\"smoke\",\"provider_id\":\"$PROV_ID\",\"tools_enabled\":false}")
echo "$SESS"
SESS_ID=$(echo "$SESS" | python -c "import sys,json;print(json.load(sys.stdin)['id'])")

echo "==> chat (streamed)"
curl -sN -X POST "$BASE_URL/api/sessions/$SESS_ID/chat" -H "Content-Type: application/json" \
  -d '{"message":"say hi in one word"}'

if [ -n "${KB_FILE:-}" ]; then
  echo "==> create KB"
  KB=$(curl -s -X POST "$BASE_URL/api/kb" -H "Content-Type: application/json" \
    -d "{\"name\":\"smoke-kb\",\"embed_provider_id\":\"$PROV_ID\"}")
  KB_ID=$(echo "$KB" | python -c "import sys,json;print(json.load(sys.stdin)['id'])")

  echo "==> upload $KB_FILE"
  curl -s -X POST "$BASE_URL/api/kb/$KB_ID/documents" -F "file=@$KB_FILE"; echo

  echo "==> waiting for ingest…"
  for i in $(seq 1 20); do
    sleep 2
    curl -s "$BASE_URL/api/kb/$KB_ID/documents"; echo
  done
fi

echo "==> smoke OK"
