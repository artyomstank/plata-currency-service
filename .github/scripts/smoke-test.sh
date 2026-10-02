#!/usr/bin/env bash
set -euo pipefail

base_url="${SMOKE_BASE_URL:-http://localhost:18080}"

check_endpoint() {
    local endpoint="$1"
    local expected_status="$2"
    local response

    response="$(curl --fail --silent --show-error \
        --connect-timeout 2 --max-time 5 \
        --retry 20 --retry-delay 1 --retry-max-time 60 --retry-connrefused \
        "${base_url}${endpoint}")"
    printf '%s\n' "$response" | jq --exit-status \
        --arg status "$expected_status" '.status == $status' >/dev/null
    printf '%s: %s\n' "$endpoint" "$expected_status"
}

check_endpoint /healthz ok
check_endpoint /readyz ready
