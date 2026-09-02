#!/bin/sh
set -eu

case "${POMEGRANATE_ROLE:-}" in
  central) port="${POMEGRANATE_LISTEN_PORT:-5033}" ;;
  operator) port="${POMEGRANATE_LISTEN_PORT:-5041}" ;;
  *) exit 1 ;;
esac

db_path="${DB_PATH:-/var/lib/pomegranate/${POMEGRANATE_ROLE}.db}"
db_dir="$(dirname "$db_path")"

test -d "$db_dir"
test -w "$db_dir"
if [ -e "$db_path" ]; then
  test -r "$db_path"
  test -w "$db_path"
fi

wget -q -T 3 -O /dev/null "http://127.0.0.1:${port}/"

