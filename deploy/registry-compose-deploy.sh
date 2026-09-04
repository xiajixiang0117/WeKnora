#!/usr/bin/env bash
# Pull pre-built application images from a registry and update only app services.
set -Eeuo pipefail

readonly SERVICES=(frontend app docreader)
readonly HEALTH_CHECK_ATTEMPTS=48
readonly HEALTH_CHECK_INTERVAL_SECONDS=5

requested_version="${1:-}"

fail() {
    echo "ERROR: $*" >&2
    exit 1
}

is_safe_image_tag() {
    [[ "$1" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]
}

require_deployment_arguments() {
    [[ -n "$requested_version" ]] || fail "Usage: $0 <version>."
    is_safe_image_tag "$requested_version" || fail "Version must be a valid Docker image tag."
}

resolve_deploy_path() {
    local script_directory
    script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    DEPLOY_PATH="${DEPLOY_PATH:-$(cd "$script_directory/.." && pwd)}"
}

compose() {
    (
        cd "$DEPLOY_PATH"
        docker compose \
            -f docker-compose.yml \
            -f docker-compose.override.yml \
            "$@"
    )
}

print_diagnostics() {
    local exit_code="$?"
    trap - ERR
    set +e
    echo "Deployment failed; collecting Compose diagnostics." >&2
    compose ps >&2
    compose logs --tail=100 "${SERVICES[@]}" >&2
    exit "$exit_code"
}

acquire_deployment_lock() {
    command -v flock >/dev/null || fail "flock is required on the deployment host."
    mkdir -p "$DEPLOY_PATH/.ci"
    exec 9>"$DEPLOY_PATH/.ci/registry-compose-deploy.lock"
    flock -n 9 || fail "Another deployment is already running."
}

wait_for_service_health() {
    local service_name="$1"
    local attempt container_id health_status

    for ((attempt = 1; attempt <= HEALTH_CHECK_ATTEMPTS; attempt++)); do
        container_id="$(compose ps -q "$service_name")"
        if [[ -n "$container_id" ]]; then
            health_status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$container_id")"
            case "$health_status" in
                healthy) echo "${service_name} is healthy."; return 0 ;;
                unhealthy) echo "ERROR: ${service_name} reported unhealthy." >&2; return 1 ;;
                starting) ;;
                *) echo "ERROR: ${service_name} has no usable Docker health status (${health_status})." >&2; return 1 ;;
            esac
        fi
        sleep "$HEALTH_CHECK_INTERVAL_SECONDS"
    done

    echo "ERROR: Timed out waiting for ${service_name} to become healthy." >&2
    return 1
}

wait_for_frontend_http() {
    local frontend_port attempt
    frontend_port="$(compose port frontend 80 | awk -F: 'NR == 1 { print $NF }')"
    [[ "$frontend_port" =~ ^[0-9]+$ ]] || fail "Could not resolve the frontend host port."

    for ((attempt = 1; attempt <= HEALTH_CHECK_ATTEMPTS; attempt++)); do
        if curl --fail --silent --show-error --max-time 5 "http://127.0.0.1:${frontend_port}/" >/dev/null; then
            echo "frontend is reachable at localhost:${frontend_port}."
            return 0
        fi
        sleep "$HEALTH_CHECK_INTERVAL_SECONDS"
    done

    echo "ERROR: Timed out waiting for frontend HTTP to become reachable." >&2
    return 1
}

require_deployment_arguments
resolve_deploy_path

[[ -f "$DEPLOY_PATH/docker-compose.yml" ]] || fail "docker-compose.yml was not found in DEPLOY_PATH."
[[ -f "$DEPLOY_PATH/docker-compose.override.yml" ]] || fail "docker-compose.override.yml was not found in DEPLOY_PATH."

acquire_deployment_lock
trap print_diagnostics ERR

export WEKNORA_VERSION="$requested_version"
export WEKNORA_IMAGE_PREFIX="${WEKNORA_IMAGE_PREFIX:-wechatopenai}"

echo "Validating Compose configuration for ${WEKNORA_IMAGE_PREFIX}/weknora-*:${WEKNORA_VERSION}."
compose config -q

echo "Pulling pre-built application images."
compose pull "${SERVICES[@]}"
compose up -d --no-build --no-deps docreader app
wait_for_service_health docreader
wait_for_service_health app

compose up -d --no-build --no-deps frontend
wait_for_frontend_http

deployment_time="$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
printf 'registry_prefix=%s\nversion=%s\ndeployed_at=%s\n' \
    "$WEKNORA_IMAGE_PREFIX" "$WEKNORA_VERSION" "$deployment_time" \
    >"$DEPLOY_PATH/.ci/current-registry-deployment.env"
echo "Deployment succeeded: ${WEKNORA_IMAGE_PREFIX}/weknora-*:${WEKNORA_VERSION}."
