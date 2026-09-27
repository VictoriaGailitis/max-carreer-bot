#!/usr/bin/env bash
set -euo pipefail

for program in docker curl jq python3; do
  command -v "$program" >/dev/null || { printf '%s is required\n' "$program" >&2; exit 1; }
done

cd "$(dirname "$0")/../.."
docker build --target ops -t max-carreer-bot-ops . >/dev/null
docker build --target authapi -t max-carreer-bot-authapi . >/dev/null

smoke_root=$(mktemp -d /tmp/max-carreer-auth.XXXXXX)
network_name="max-carreer-smoke-$(date +%s)-$$"
db_name="max-carreer-db-$$"
db_id=
api_id=
cleanup() {
  if [[ -n "$api_id" ]]; then docker rm -f "$api_id" >/dev/null 2>&1 || true; fi
  if [[ -n "$db_id" ]]; then docker rm -f "$db_id" >/dev/null 2>&1 || true; fi
  docker network rm "$network_name" >/dev/null 2>&1 || true
  rm -rf "$smoke_root"
}
trap cleanup EXIT

mkdir "$smoke_root/secrets"
docker network create "$network_name" >/dev/null
db_id=$(docker run -d --network "$network_name" --name "$db_name" \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  postgres:17.11-bookworm@sha256:639ab7ceb90e13123085b741fb31ef493fba25463002f6da665352e7b534b652)
ready=0
for _ in {1..30}; do
  if docker exec "$db_id" pg_isready -U postgres -d postgres >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then docker logs "$db_id"; exit 1; fi
docker exec -i "$db_id" psql -U postgres -d postgres -v ON_ERROR_STOP=1 < migrations/0001_user_core.up.sql >/dev/null
docker exec -i "$db_id" psql -U postgres -d postgres -v ON_ERROR_STOP=1 < migrations/0002_profile_state.up.sql >/dev/null
docker exec -i "$db_id" psql -U postgres -d postgres -v ON_ERROR_STOP=1 < migrations/0003_catalog.up.sql >/dev/null
docker exec -i "$db_id" psql -U postgres -d postgres -v ON_ERROR_STOP=1 < migrations/0004_bot_inbox.up.sql >/dev/null

printf 'test-bot-token\n' > "$smoke_root/secrets/bot-token"
printf 'smoke-webhook-secret\n' > "$smoke_root/secrets/webhook-secret"
docker run --rm --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$smoke_root/secrets,dst=/secrets" \
  max-carreer-bot-ops generate-key -out /secrets/lookup-key >/dev/null
docker run --rm --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$smoke_root/secrets,dst=/secrets" \
  max-carreer-bot-ops generate-key -out /secrets/data-key >/dev/null
printf 'postgres://postgres@%s:5432/postgres?sslmode=disable\n' "$db_name" > "$smoke_root/secrets/db-url"
chmod 600 "$smoke_root/secrets/bot-token" "$smoke_root/secrets/webhook-secret" "$smoke_root/secrets/db-url"

api_id=$(docker run -d --network "$network_name" --user "$(id -u):$(id -g)" \
  -e APP_ENV=local -e HTTP_ADDR=0.0.0.0:8081 \
  -e CORS_ALLOWED_ORIGINS=http://localhost:5173 \
  -e MAX_BOT_TOKEN_FILE=/secrets/bot-token \
  -e MAX_WEBHOOK_SECRET_FILE=/secrets/webhook-secret \
  -e IDENTITY_LOOKUP_KEY_V1_FILE=/secrets/lookup-key \
  -e DATA_KEY_V1_FILE=/secrets/data-key \
  -e DATABASE_URL_FILE=/secrets/db-url \
  --mount "type=bind,src=$smoke_root/secrets,dst=/secrets,readonly" \
  -p 127.0.0.1:18081:8081 max-carreer-bot-authapi)
ready=0
for _ in {1..20}; do
  if curl -fsS http://127.0.0.1:18081/healthz >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then docker logs "$api_id"; exit 1; fi

# Browser preflight reaches the same production handler as authenticated requests.
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X OPTIONS -H 'Origin: http://localhost:5173' -H 'Access-Control-Request-Method: PUT' -H 'Access-Control-Request-Headers: Authorization,Content-Type,Idempotency-Key' http://127.0.0.1:18081/api/v1/me/draft)" == 204 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X OPTIONS -H 'Origin: https://foreign.test' -H 'Access-Control-Request-Method: PUT' http://127.0.0.1:18081/api/v1/me/draft)" == 403 ]]

webhook=http://127.0.0.1:18081/api/v1/webhooks/max
event='{"update_type":"bot_started","timestamp":1780000000000,"chat_id":777,"user":{"user_id":99887766}}'
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d "$event" "$webhook")" == 401 ]]
for _ in 1 2; do
  [[ "$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'X-Max-Bot-Api-Secret: smoke-webhook-secret' -d "$event" "$webhook")" == 200 ]]
done
[[ "$(docker exec "$db_id" psql -U postgres -d postgres -At -c "SELECT count(*) FROM bot_inbox WHERE status='pending' AND payload_ciphertext IS NOT NULL")" == 1 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -H 'X-Max-Bot-Api-Secret: smoke-webhook-secret' -d '{' "$webhook")" == 400 ]]
docker build --target test -t max-carreer-bot-test . >/dev/null
docker run --rm --network "$network_name" --user "$(id -u):$(id -g)" \
  -e BOT_INBOX_TEST_DB_URL_FILE=/secrets/db-url \
  -e BOT_INBOX_TEST_DATA_KEY_FILE=/secrets/data-key \
  -e GOCACHE=/tmp/go-cache \
  --mount "type=bind,src=$smoke_root/secrets,dst=/secrets,readonly" \
  max-carreer-bot-test go test ./internal/maxbot -run '^TestInboxIntegration$' -count=1

signed_data() {
  python3 - "$1" <<'PY'
import hashlib, hmac, json, sys, time, urllib.parse
values = {'auth_date': str(int(time.time())), 'user': json.dumps({'id': int(sys.argv[1])}, separators=(',', ':'))}
check = '\n'.join(f'{key}={values[key]}' for key in sorted(values))
secret = hmac.new(b'WebAppData', b'test-bot-token', hashlib.sha256).digest()
signature = hmac.new(secret, check.encode(), hashlib.sha256).hexdigest()
print('&'.join(f'{key}={urllib.parse.quote(value)}' for key, value in values.items()) + '&hash=' + signature)
PY
}

login() {
  local body
  body=$(jq -n --arg value "$(signed_data "$1")" '{init_data:$value}')
  curl -fsS -H 'Content-Type: application/json' -d "$body" http://127.0.0.1:18081/api/v1/auth/max
}

first=$(login 67890)
first_again=$(login 67890)
second=$(login 67891)
first_user=$(jq -r '.user_id' <<< "$first")
second_user=$(jq -r '.user_id' <<< "$second")
[[ "$first_user" == "$(jq -r '.user_id' <<< "$first_again")" ]]
[[ "$first_user" != "$second_user" ]]
token_one=$(jq -r '.token' <<< "$first")
token_one_again=$(jq -r '.token' <<< "$first_again")
token_two=$(jq -r '.token' <<< "$second")
[[ ${#token_one} -eq 43 && ${#token_one_again} -eq 43 && ${#token_two} -eq 43 ]]

preview_status() {
  curl -sS -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d '{"directions":["backend"]}' http://127.0.0.1:18081/api/v1/questionnaire/preview
}
[[ "$(preview_status "$token_one")" == 200 ]]
[[ "$(preview_status "$token_two")" == 200 ]]

base=http://127.0.0.1:18081/api/v1
metadata=$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/questionnaire")
[[ "$(jq -r '.selection_version == 1 and (.directions|length) == 9 and (.general_questions|length) == 5' <<< "$metadata")" == true ]]
first_state=$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me")
[[ "$(jq -r '.profile == null and .draft == null and .draft_revision == 0' <<< "$first_state")" == true ]]
preview=$(curl -fsS -H "Authorization: Bearer $token_one_again" -H 'Content-Type: application/json' \
  -d '{"directions":["backend"]}' "$base/questionnaire/preview")
draft_body=$(jq -n --argjson preview "$preview" '{revision:0,draft:{current_step:8,directions:["backend"],goals:["course"],education_stage:"undergraduate",study_year:2,preferred_format:"online",backend_languages:["go"],skills:($preview.questions|map({key:.id,value:0})|from_entries)}}')
draft=$(curl -fsS -X PUT -H "Authorization: Bearer $token_one_again" -H 'Content-Type: application/json' \
  -d "$draft_body" "$base/me/draft")
[[ "$(jq -r '.revision == 1 and (.draft.question_ids|length) == 8 and .draft.base_profile_revision == 0' <<< "$draft")" == true ]]
stale_status=$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $token_one_again" \
  -H 'Content-Type: application/json' -d "$draft_body" "$base/me/draft")
[[ "$stale_status" == 409 ]]
[[ "$(jq -r '.profile == null and .draft != null' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me")")" == true ]]
[[ "$(jq -r '.profile == null and .draft == null' <<< "$(curl -fsS -H "Authorization: Bearer $token_two" "$base/me")")" == true ]]
key=8b0de594-7a27-4e3f-a025-7f14ad009e6e
complete=$(curl -fsS -H "Authorization: Bearer $token_one_again" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $key" -d '{"draft_revision":1}' "$base/me/onboarding/complete")
replay=$(curl -fsS -H "Authorization: Bearer $token_one_again" -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $key" -d '{"draft_revision":1}' "$base/me/onboarding/complete")
[[ "$complete" == "$replay" ]]
[[ "$(jq -r '.revision == 1 and .profile.skills != null and .profile.completed_at != null' <<< "$complete")" == true ]]
different_body_status=$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token_one_again" \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $key" -d '{"draft_revision":2}' "$base/me/onboarding/complete")
[[ "$different_body_status" == 409 ]]
[[ "$(jq -r '.profile_revision == 1 and .draft == null and .draft_revision == 2' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me")")" == true ]]

docker run --rm --network "$network_name" --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$PWD/testdata,dst=/fixture,readonly" \
  --mount "type=bind,src=$smoke_root/secrets,dst=/secrets,readonly" \
  max-carreer-bot-ops catalog import -file /fixture/catalog-demo.v1.json \
  -database-url-file /secrets/db-url -allow-domain example.test >/dev/null
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token_one_again" "$base/recommendations")" == 503 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:18081/readyz)" == 503 ]]
jq '.is_demo=false | .items |= map(.is_demo=false) | .items += [(.items[2] | .id="demo-closed-event" | .availability="closed"), (.items[0] | .id="demo-draft-course" | .publication_status="draft")]' \
  testdata/catalog-demo.v1.json > "$smoke_root/live.json"
docker run --rm --network "$network_name" --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$smoke_root,dst=/work,readonly" \
  max-carreer-bot-ops catalog import -file /work/live.json \
  -database-url-file /work/secrets/db-url -allow-domain example.test >/dev/null
repeat_import=$(docker run --rm --network "$network_name" --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$smoke_root,dst=/work,readonly" \
  max-carreer-bot-ops catalog import -file /work/live.json \
  -database-url-file /work/secrets/db-url -allow-domain example.test)
[[ "$repeat_import" == catalog\ unchanged:* ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:18081/readyz)" == 200 ]]
recommendations=$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/recommendations")
catalog_version=$(jq -r '.catalog_version' <<< "$recommendations")
[[ "$(jq -r '.items|length' <<< "$recommendations")" == 3 ]]
[[ "$(jq -r '.items[0].id' <<< "$recommendations")" == demo-course-backend ]]
[[ "$(jq -r '.has_direction_matches and .profile_revision == 1' <<< "$recommendations")" == true ]]
[[ "$(jq -r '.items|length' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/opportunities?type=event")")" == 1 ]]
[[ "$(jq -r '.item.effective_status' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/opportunities/demo-closed-event")")" == closed ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token_one_again" "$base/opportunities/demo-draft-course")" == 404 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token_two" "$base/recommendations")" == 409 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $token_one_again" "$base/opportunities?type=bad")" == 400 ]]
jq '.items[1].id=.items[0].id' "$smoke_root/live.json" > "$smoke_root/invalid.json"
if docker run --rm --network "$network_name" --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$smoke_root,dst=/work,readonly" \
  max-carreer-bot-ops catalog import -file /work/invalid.json \
  -database-url-file /work/secrets/db-url -allow-domain example.test >/dev/null 2>&1; then
  printf 'invalid catalog was imported\n' >&2
  exit 1
fi
[[ "$(jq -r '.catalog_version' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/recommendations")")" == "$catalog_version" ]]
[[ "$(docker exec "$db_id" psql -U postgres -d postgres -At -c 'SELECT count(*) FROM catalog_versions')" == 2 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-course-backend")" == 204 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-course-backend")" == 204 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-event-design")" == 204 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-closed-event")" == 409 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-draft-course")" == 404 ]]
[[ "$(jq -r '.favorites_revision == 2 and (.items|length) == 2' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me/favorites")")" == true ]]

edit_body=$(jq -n '{revision:2,draft:{current_step:1,directions:["frontend"]}}')
curl -fsS -X PUT -H "Authorization: Bearer $token_one_again" -H 'Content-Type: application/json' -d "$edit_body" "$base/me/draft" >/dev/null
[[ "$(jq -r '.profile_revision == 1 and .draft != null' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me")")" == true ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $token_one_again" "$base/me/draft")" == 204 ]]
[[ "$(jq -r '.profile_revision == 1 and .draft == null and .draft_revision == 4' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me")")" == true ]]
[[ "$(jq -r '.favorites_revision == 2 and (.items|length) == 2' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me/favorites")")" == true ]]
jq '.items |= map(select(.id != "demo-course-backend") | if .id == "demo-event-design" then .availability="closed" else . end)' \
  "$smoke_root/live.json" > "$smoke_root/updated.json"
docker run --rm --network "$network_name" --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$smoke_root,dst=/work,readonly" \
  max-carreer-bot-ops catalog import -file /work/updated.json \
  -database-url-file /work/secrets/db-url -allow-domain example.test >/dev/null
favorites_after_update=$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me/favorites")
[[ "$(jq -r '[.items[] | select(.id == "demo-course-backend")][0] | .effective_status == "unavailable" and .item == null' <<< "$favorites_after_update")" == true ]]
[[ "$(jq -r '[.items[] | select(.id == "demo-event-design")][0].effective_status' <<< "$favorites_after_update")" == closed ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-course-backend")" == 204 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-course-backend")" == 204 ]]
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $token_one_again" "$base/me/favorites/demo-event-design")" == 204 ]]
[[ "$(jq -r '.favorites_revision == 4 and (.items|length) == 0' <<< "$(curl -fsS -H "Authorization: Bearer $token_one_again" "$base/me/favorites")")" == true ]]
logout_status=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE \
  -H "Authorization: Bearer $token_one" http://127.0.0.1:18081/api/v1/auth/session)
[[ "$logout_status" == 204 ]]
[[ "$(preview_status "$token_one")" == 401 ]]
[[ "$(preview_status "$token_one_again")" == 200 ]]
[[ "$(preview_status "$token_two")" == 200 ]]

counts=$(docker exec "$db_id" psql -U postgres -d postgres -At -c \
  'SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM user_state), (SELECT count(*) FROM sessions), (SELECT count(*) FROM idempotency_records)')
[[ "$counts" == '2|2|3|1' ]]
dump=$(docker exec "$db_id" pg_dump -U postgres -d postgres --data-only)
case "$dump" in
  *"$token_one"*|*"$token_one_again"*|*"$token_two"*|*67890*|*67891*|*99887766*|*BACK-01*)
    printf 'raw token, MAX ID or profile data found in database dump\n' >&2
    exit 1
    ;;
esac
[[ "$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $token_one_again" "$base/me/data")" == 204 ]]
[[ "$(preview_status "$token_one_again")" == 401 ]]
[[ "$(preview_status "$token_two")" == 200 ]]
[[ "$(docker exec "$db_id" psql -U postgres -d postgres -At -c 'SELECT count(*) FROM users')" == 1 ]]
printf 'MAX auth and webhook, encrypted state and bot inbox, catalog import, ranking, reset and isolation: ok\n'
