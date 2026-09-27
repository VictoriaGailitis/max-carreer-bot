#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

for program in docker python3; do
  command -v "$program" >/dev/null || { printf '%s is required\n' "$program" >&2; exit 1; }
done

python3 - <<'PY'
import hashlib
import hmac
import json
import pathlib
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

base = "http://127.0.0.1:18080"
bot_token = pathlib.Path(".local/secrets/bot-token").read_text().strip()
if not bot_token.startswith("local-only-"):
    raise SystemExit("smoke-compose requires local-only bot credentials")

def request(path, method="GET", body=None, bearer=None):
    headers = {}
    if body is not None:
        headers["Content-Type"] = "application/json"
    if bearer:
        headers["Authorization"] = "Bearer " + bearer
    req = urllib.request.Request(base + path, data=body, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=5) as response:
        return response.status, json.load(response)

status, health = request("/healthz")
assert status == 200 and health == {"status": "ok"}

values = {
    "auth_date": str(int(time.time())),
    "user": json.dumps({"id": 987654321}, separators=(",", ":")),
}
check = "\n".join(f"{key}={values[key]}" for key in sorted(values))
secret = hmac.new(b"WebAppData", bot_token.encode(), hashlib.sha256).digest()
signature = hmac.new(secret, check.encode(), hashlib.sha256).hexdigest()
init_data = "&".join(f"{key}={urllib.parse.quote(value)}" for key, value in values.items()) + "&hash=" + signature
status, login = request("/api/v1/auth/max", "POST", json.dumps({"init_data": init_data}).encode())
assert status == 200 and login["user_id"] and login["token"]
status, before = request("/api/v1/me", bearer=login["token"])
assert status == 200 and before["profile"] is None

rerun = subprocess.run(["docker", "compose", "run", "--rm", "migrate"], capture_output=True, text=True, check=True)
assert "migrations ready: 4" in rerun.stderr and "applied migration" not in rerun.stderr

sql = "SELECT (SELECT count(*) FROM schema_migrations), has_table_privilege('navigator_api','users','INSERT'), has_table_privilege('navigator_api','opportunities','INSERT'), has_table_privilege('navigator_api','schema_migrations','SELECT'), has_schema_privilege('navigator_api','public','CREATE')"
shell = 'export PGPASSWORD="$(cat /run/secrets/db_admin_password)"; psql -U postgres -d navigator -At -c "$1"'
privileges = subprocess.check_output(["docker", "compose", "exec", "-T", "db", "sh", "-c", shell, "sh", sql], text=True).strip()
assert privileges == "4|t|f|f|f", privileges

subprocess.run(["docker", "compose", "restart", "api"], check=True, stdout=subprocess.DEVNULL)
for _ in range(30):
    try:
        status, after = request("/api/v1/me", bearer=login["token"])
        if status == 200:
            break
    except (urllib.error.URLError, TimeoutError):
        pass
    time.sleep(1)
else:
    raise SystemExit("API did not recover after restart")
assert after == before
print("Compose proxy, migration replay, restricted API role and session persistence: ok")
PY
