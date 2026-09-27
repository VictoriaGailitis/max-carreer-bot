#!/usr/bin/env python3
"""Produce a fresh signed synthetic MAX login body for the local Compose stack."""
import argparse
import hashlib
import hmac
import json
from pathlib import Path
import time
import urllib.parse


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--user-id', type=int, default=999000111)
    args = parser.parse_args()
    if not 0 < args.user_id <= 9007199254740991:
        parser.error('user-id must be a positive JavaScript-safe integer')
    token = (Path(__file__).resolve().parents[2] / '.local' / 'secrets' / 'bot-token').read_text().strip()
    if not token.startswith('local-only-'):
        parser.error('requires local-only bot credentials created by bootstrap-local.sh')
    values = {
        'auth_date': str(int(time.time())),
        'user': json.dumps({'id': args.user_id}, separators=(',', ':')),
    }
    check = '\n'.join(f'{key}={values[key]}' for key in sorted(values))
    key = hmac.new(b'WebAppData', token.encode(), hashlib.sha256).digest()
    values['hash'] = hmac.new(key, check.encode(), hashlib.sha256).hexdigest()
    print(json.dumps({'init_data': urllib.parse.urlencode(values)}))


if __name__ == '__main__':
    main()
