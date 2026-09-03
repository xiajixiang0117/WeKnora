#!/usr/bin/env bash
# Runs on the deployment host. It updates a checked-out source branch, builds
# the application images locally, and updates only the application services.
set -Eeuo pipefail

readonly SERVICES=(frontend app docreader)
readonly CI_DIRECTORY_NAME=".ci"
readonly HEALTH_CHECK_ATTEMPTS=48
readonly HEALTH_CHECK_INTERVAL_SECONDS=5
readonly FRONTEND_NODE_IMAGE="${FRONTEND_NODE_IMAGE:-node:24-bookworm-slim}"

requested_branch="${1:-}"
requested_version="${2:-}"

fail() {
    echo "ERROR: $*" >&2
    exit 1
}

is_safe_image_tag() {
    [[ "$1" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]]
}

require_deployment_arguments() {
    [[ -n "$requested_branch" ]] || fail "Usage: $0 <branch> <version>."
    [[ -n "$requested_version" ]] || fail "Usage: $0 <branch> <version>."
    [[ "$requested_branch" != -* && "$requested_branch" != refs/* ]] \
        || fail "Branch name is invalid: ${requested_branch}."
    git check-ref-format "refs/heads/${requested_branch}" >/dev/null \
        || fail "Branch name is invalid: ${requested_branch}."
    is_safe_image_tag "$requested_version" \
        || fail "Version must be a valid Docker image tag."
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
    mkdir -p "$DEPLOY_PATH/$CI_DIRECTORY_NAME"
    exec 9>"$DEPLOY_PATH/$CI_DIRECTORY_NAME/source-compose-deploy.lock"
    flock -n 9 || fail "Another source deployment is already running."
}

update_source_branch() {
    echo "Updating source branch ${requested_branch}."
    git -C "$DEPLOY_PATH" fetch --prune origin

    if git -C "$DEPLOY_PATH" show-ref --verify --quiet "refs/heads/${requested_branch}"; then
        git -C "$DEPLOY_PATH" checkout "$requested_branch"
    else
        git -C "$DEPLOY_PATH" checkout --track "origin/${requested_branch}"
    fi

    git -C "$DEPLOY_PATH" pull --ff-only origin "$requested_branch"
}

build_frontend_distribution() {
    local frontend_directory npm_cache_directory

    frontend_directory="${DEPLOY_PATH}/frontend"
    npm_cache_directory="${DEPLOY_PATH}/${CI_DIRECTORY_NAME}/npm-cache"
    install -d -m 700 "$npm_cache_directory"

    echo "Building frontend assets with ${FRONTEND_NODE_IMAGE}."
    docker run --rm \
        --user "$(id -u):$(id -g)" \
        --env HOME=/tmp \
        --env npm_config_cache=/tmp/npm-cache \
        --env VITE_IS_DOCKER=true \
        --env VITE_FRONTEND_COMMIT="$deployment_commit" \
        --volume "${frontend_directory}:/workspace" \
        --volume "${npm_cache_directory}:/tmp/npm-cache" \
        --workdir /workspace \
        "$FRONTEND_NODE_IMAGE" \
        sh -c 'npm ci && npm run build'
}

wait_for_service_health() {
    local service_name="$1"
    local attempt container_id health_status

    for ((attempt = 1; attempt <= HEALTH_CHECK_ATTEMPTS; attempt++)); do
        container_id="$(compose ps -q "$service_name")"
        if [[ -n "$container_id" ]]; then
            health_status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$container_id")"
            case "$health_status" in
                healthy)
                    echo "${service_name} is healthy."
                    return 0
                    ;;
                unhealthy)
                    echo "ERROR: ${service_name} reported unhealthy." >&2
                    return 1
                    ;;
                starting)
                    ;;
                *)
                    echo "ERROR: ${service_name} has no usable Docker health status (${health_status})." >&2
                    return 1
                    ;;
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

[[ -d "$DEPLOY_PATH/.git" ]] || fail "DEPLOY_PATH must be a Git working tree: ${DEPLOY_PATH}."
[[ -f "$DEPLOY_PATH/docker-compose.yml" ]] || fail "docker-compose.yml was not found in DEPLOY_PATH."
[[ -f "$DEPLOY_PATH/docker-compose.override.yml" ]] || fail "docker-compose.override.yml was not found in DEPLOY_PATH."

acquire_deployment_lock
trap print_diagnostics ERR

update_source_branch

deployment_commit="$(git -C "$DEPLOY_PATH" rev-parse HEAD)"
deployment_build_time="$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
deployment_go_version="$(go version 2>/dev/null || echo unknown)"
export WEKNORA_VERSION="$requested_version"
export WEKNORA_COMMIT_ID="$deployment_commit"
export WEKNORA_BUILD_TIME="$deployment_build_time"
export WEKNORA_GO_VERSION="$deployment_go_version"

echo "Validating Compose configuration for version ${WEKNORA_VERSION}."
compose config -q

echo "Building frontend, app, and docreader from ${deployment_commit}."
build_frontend_distribution
compose build "${SERVICES[@]}"

echo "Updating docreader and app without touching dependencies."
compose up -d --no-build --no-deps docreader app
wait_for_service_health docreader
wait_for_service_health app

echo "Updating frontend after the backend services are healthy."
compose up -d --no-build --no-deps frontend
wait_for_frontend_http

printf 'branch=%s\nversion=%s\ncommit=%s\ndeployed_at=%s\n' \
    "$requested_branch" "$WEKNORA_VERSION" "$deployment_commit" "$deployment_build_time" \
    >"$DEPLOY_PATH/$CI_DIRECTORY_NAME/current-source-deployment.env"
echo "Deployment succeeded: branch ${requested_branch}, version ${WEKNORA_VERSION}, commit ${deployment_commit}."
