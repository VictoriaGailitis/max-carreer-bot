#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."
if [[ -e .env || -e .local/secrets ]]; then
  printf 'Refusing to replace existing .env or .local/secrets directory\n' >&2
  exit 1
fi

python3 - <<'PY'
import os
import pathlib
import secrets
import urllib.parse

root = pathlib.Path.cwd()
local_dir = root / ".local"
local_dir.mkdir(mode=0o700, exist_ok=True)
secret_dir = local_dir / "secrets"
secret_dir.mkdir(mode=0o700)

def write(name: str, value: str) -> None:
    path = secret_dir / name
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        stream.write(value + "\n")

admin_password = secrets.token_urlsafe(36)
app_password = secrets.token_urlsafe(36)
write("db-admin-password", admin_password)
write("database-url-migrate", "postgres://postgres:" + urllib.parse.quote(admin_password, safe="") + "@db:5432/navigator?sslmode=disable")
write("database-url-api", "postgres://navigator_api:" + urllib.parse.quote(app_password, safe="") + "@db:5432/navigator?sslmode=disable")
write("bot-token", "local-only-" + secrets.token_urlsafe(24))
write("webhook-secret", secrets.token_urlsafe(32))
write("lookup-key", secrets.token_hex(32))
write("data-key", secrets.token_hex(32))

env = root / ".env"
fd = os.open(env, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, "w", encoding="utf-8") as stream:
    stream.write(f"APP_UID={os.getuid()}\nAPP_GID={os.getgid()}\n")
    stream.write("PUBLIC_DOMAIN=\nMAX_APP_BOT_USERNAME=\nCORS_ALLOWED_ORIGINS=\nFRONTEND_DIST=./frontend/dist\n")
print("Created local-only .env and secret files. No secret values were printed.")
PY
