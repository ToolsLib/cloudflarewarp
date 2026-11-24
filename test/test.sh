#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
project="cloudflarewarp-test-$$"

compose() {
  docker compose --project-name "$project" --file compose.yml "$@"
}

cleanup() {
  local status=$?
  if ((status != 0)); then
    compose logs --no-color >&2 || true
  fi
  compose down --timeout 5 || return 1
  return "$status"
}

request() {
  local route=$1 client_ip=$2
  # Whoami echoes HTTP header lines with CRLF endings.
  compose exec -T traefik wget -qO- -T 3 \
    --header "CF-Connecting-IP: $client_ip" \
    --header 'CF-Visitor: {"scheme":"https"}' \
    "http://127.0.0.1:8000/$route" | sed $'s/\r$//'
}

require_header() {
  local response=$1 expected=$2
  if ! grep -Fqx -- "$expected" <<< "$response"; then
    printf 'Expected header %s\n%s\n' "$expected" "$response" >&2
    return 1
  fi
}

for format in yml toml; do
  (
    export TEST_CONFIG_FORMAT="$format"
    trap cleanup EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    compose up -d

    ready=false
    for ((attempt = 0; attempt < 20; attempt++)); do
      if request trusted 192.0.2.10 >/dev/null 2>&1; then
        ready=true
        break
      fi
      sleep 1
    done
    if [[ $ready == false ]]; then
      printf 'Traefik did not serve the %s fixture\n' "$format" >&2
      exit 1
    fi

    for client_ip in 192.0.2.10 2001:db8::10; do
      response=$(request trusted "$client_ip")
      require_header "$response" 'X-Is-Trusted: yes'
      require_header "$response" "X-Real-Ip: $client_ip"
      require_header "$response" 'X-Forwarded-Proto: https'
      forwarded_for=$(sed -n 's/^X-Forwarded-For: //p' <<< "$response")
      if [[ ${forwarded_for%%,*} != "$client_ip" ]]; then
        printf 'Expected X-Forwarded-For to start with %s\n%s\n' "$client_ip" "$response" >&2
        exit 1
      fi
    done

    for route in untrusted defaults; do
      response=$(request "$route" 192.0.2.10)
      require_header "$response" 'X-Is-Trusted: no'
      require_header "$response" 'X-Real-Ip: 127.0.0.1'
    done

    printf 'Traefik plugin checks passed for %s\n' "$format"
  )
done
