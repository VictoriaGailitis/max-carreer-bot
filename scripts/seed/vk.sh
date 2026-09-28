#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

usage() {
  printf 'Usage: bash scripts/seed/vk.sh [--collect-only | --file CATALOG_JSON]\n'
}
collect_only=0
input_file=
case "${1:-}" in
  '') [[ $# == 0 ]] || { usage >&2; exit 2; } ;;
  --collect-only) [[ $# == 1 ]] || { usage >&2; exit 2; }; collect_only=1 ;;
  --file) [[ $# == 2 ]] || { usage >&2; exit 2; }; input_file=$2 ;;
  --help|-h) usage; exit 0 ;;
  *) usage >&2; exit 2 ;;
esac
command -v python3 >/dev/null || { printf 'Python 3 is required\n' >&2; exit 1; }
if [[ "$collect_only" == 0 ]]; then
  command -v docker >/dev/null || { printf 'Docker is required\n' >&2; exit 1; }
  python3 - <<'PY'
from pathlib import Path
path=Path('.local/secrets/bot-token')
if not path.is_file() or not path.read_text().strip().startswith('local-only-'):
    raise SystemExit('VK seed requires the local-only Compose environment; run make bootstrap first')
PY
fi
if [[ -n "$input_file" ]]; then
  python3 - "$input_file" <<'PY'
from pathlib import Path
import json,sys
from shutil import copyfile
source=Path(sys.argv[1]).resolve()
target=Path('.local/catalog/vk.json').resolve()
with source.open(encoding='utf-8') as stream:
    dataset=json.load(stream)
if dataset.get('is_demo') is not False or not dataset.get('items'):
    raise SystemExit('Expected a non-empty source-backed catalog')
target.parent.mkdir(parents=True,exist_ok=True)
if source!=target:
    copyfile(source,target)
PY
else
  python3 scripts/seed/vk_catalog.py
fi
if [[ "$collect_only" == 1 ]]; then exit 0; fi

docker compose -f compose.yaml -f deploy/compose.tools.yaml build catalog
# Validate the whole snapshot before starting migrations or activating a catalog.
docker compose -f compose.yaml -f deploy/compose.tools.yaml run --rm --no-deps -T catalog \
  catalog validate -file /catalog/vk.json \
  -allow-domain education.vk.company -allow-domain internship.vk.company \
  -allow-domain vk.com -allow-domain ai.edu.gov.ru

docker compose up --build -d

docker compose -f compose.yaml -f deploy/compose.tools.yaml run --rm --no-deps -T catalog \
  catalog import -file /catalog/vk.json -database-url-file /run/secrets/database_url_migrate \
  -allow-domain education.vk.company -allow-domain internship.vk.company \
  -allow-domain vk.com -allow-domain ai.edu.gov.ru
