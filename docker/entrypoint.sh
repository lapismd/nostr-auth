#!/bin/sh
set -eu

load_secret() {
  name="$1"
  eval "file=\${${name}_FILE:-}"
  eval "value=\${${name}:-}"

  if [ -n "$file" ] && [ -n "$value" ]; then
    echo "$name and ${name}_FILE are mutually exclusive" >&2
    exit 64
  fi
  if [ -n "$file" ]; then
    if [ ! -r "$file" ]; then
      echo "configured secret file for $name is not readable" >&2
      exit 66
    fi
    value="$(tr -d '\r\n' < "$file")"
    export "$name=$value"
  fi
}

for secret_name in SECRET_KEY GOOGLE_CLIENT_SECRET GITHUB_CLIENT_SECRET MICROSOFT_CLIENT_SECRET; do
  load_secret "$secret_name"
done

role="${POMEGRANATE_ROLE:-}"
case "$role" in
  central)
    export PORT="${POMEGRANATE_INTERNAL_PORT:-15033}"
    public_port="${POMEGRANATE_LISTEN_PORT:-5033}"
    command=/usr/local/bin/pomegranate-central
    ;;
  operator)
    export PORT="${POMEGRANATE_LISTEN_PORT:-5041}"
    public_port="$PORT"
    command=/usr/local/bin/pomegranate-operator
    ;;
  *)
    echo "POMEGRANATE_ROLE must be central or operator" >&2
    exit 64
    ;;
esac

fifo="/tmp/pomegranate-log.$$"
mkfifo -m 0600 "$fifo"

filter_pid=
server_pid=
proxy_pid=

terminate() {
  trap - INT TERM EXIT
  [ -z "$proxy_pid" ] || kill -TERM "$proxy_pid" 2>/dev/null || true
  [ -z "$server_pid" ] || kill -TERM "$server_pid" 2>/dev/null || true
  [ -z "$filter_pid" ] || kill -TERM "$filter_pid" 2>/dev/null || true
  wait 2>/dev/null || true
  rm -f "$fifo"
}
trap 'terminate; exit 143' INT TERM
trap terminate EXIT

awk -v role="$role" -f /usr/local/share/pomegranate/log-filter.awk < "$fifo" &
filter_pid=$!
"$command" > "$fifo" 2>&1 &
server_pid=$!

if [ "$role" = central ]; then
  socat "TCP-LISTEN:${public_port},bind=0.0.0.0,reuseaddr,fork" \
    "TCP:127.0.0.1:${PORT}" &
  proxy_pid=$!
fi

while :; do
  if ! kill -0 "$server_pid" 2>/dev/null; then
    set +e
    wait "$server_pid"
    status=$?
    set -e
    exit "$status"
  fi
  if [ -n "$proxy_pid" ] && ! kill -0 "$proxy_pid" 2>/dev/null; then
    set +e
    wait "$proxy_pid"
    status=$?
    set -e
    exit "$status"
  fi
  if ! kill -0 "$filter_pid" 2>/dev/null; then
    echo "role=$role event=log_filter_failed" >&2
    exit 70
  fi
  sleep 1
done

