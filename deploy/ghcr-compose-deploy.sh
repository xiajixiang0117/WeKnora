#!/usr/bin/env bash
# Runs on the deployment host.  Its only stdin value is the GHCR pull token,
# supplied by GitHub Actions with `docker login --password-stdin` semantics.
set -Eeuo pipefail

readonly SERVICES=(frontend app docreader)
readonly CI_DIRECTORY_NAME=".ci"
readonly CURRENT_SUCCESS_FILE="current-successful.env"
readonly PREVIOUS_SUCCESS_FILE="previous-successful.env"
readonly HEALTH_CHECK_ATTEMPTS=48
readonly HEALTH_CHECK_INTERVAL_SECONDS=5

operation="${1:-}"

fail() {
    echo "ERROR: $*" >&2
    exit 1
}

require_environment() {
    local variable_name
    for variable_name in "$@"; do
        if [[ -z "${!variable_name:-}" ]]; then
            fail "Required environment variable ${variable_name} is not set."
        fi
    done
}

is_safe_image_component() {
    [[ "$1" =~ ^[a-z0-9][a-z0-9._-]*$ ]]
}

is_safe_deploy_path() {
    [[ "$1" =~ ^/[a-zA-Z0-9._/-]+$ ]]
}

require_environment DEPLOY_PATH GHCR_PULL_USERNAME WEKNORA_IMAGE_OWNER
is_safe_deploy_path "$DEPLOY_PATH" || fail "DEPLOY_PATH contains unsupported characters."
is_safe_image_component "$WEKNORA_IMAGE_OWNER" || fail "WEKNORA_IMAGE_OWNER is invalid."

case "$operation" in
    deploy | rollback) ;;
    *) fail "Operation must be deploy or rollback." ;;
esac

readonly CI_DIRECTORY="${DEPLOY_PATH}/${CI_DIRECTORY_NAME}"
readonly COMPOSE_OVERLAY="${CI_DIRECTORY}/docker-compose.ghcr.yml"
readonly CURRENT_SUCCESS_PATH="${CI_DIRECTORY}/${CURRENT_SUCCESS_FILE}"
readonly PREVIOUS_SUCCESS_PATH="${CI_DIRECTORY}/${PREVIOUS_SUCCESS_FILE}"

[[ -f "${DEPLOY_PATH}/docker-compose.yml" ]] || fail "docker-compose.yml was not found in DEPLOY_PATH."
[[ -f "${DEPLOY_PATH}/docker-compose.override.yml" ]] || fail "docker-compose.override.yml was not found in DEPLOY_PATH."
[[ -f "$COMPOSE_OVERLAY" ]] || fail "GHCR Compose overlay was not found."

compose() {
    (
        cd "$DEPLOY_PATH"
        docker compose \
            -f docker-compose.yml \
            -f docker-compose.override.yml \
            -f "${CI_DIRECTORY_NAME}/docker-compose.ghcr.yml" \
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

cleanup_docker_config() {
    local exit_code="$?"
    if [[ -n "${DEPLOY_DOCKER_CONFIG:-}" && -d "$DEPLOY_DOCKER_CONFIG" ]]; then
        rm -rf "$DEPLOY_DOCKER_CONFIG"
    fi
    exit "$exit_code"
}

trap print_diagnostics ERR
trap cleanup_docker_config EXIT

read_state_value() {
    local state_path="$1"
    local name="$2"
    awk -F= -v expected_name="$name" '$1 == expected_name { print $2; exit }' "$state_path"
}

load_previous_successful_tag() {
    [[ -f "$PREVIOUS_SUCCESS_PATH" ]] || fail "No previous successful GHCR deployment is recorded on this server."

    local previous_owner
    previous_owner="$(read_state_value "$PREVIOUS_SUCCESS_PATH" WEKNORA_IMAGE_OWNER)"
    WEKNORA_IMAGE_TAG="$(read_state_value "$PREVIOUS_SUCCESS_PATH" WEKNORA_IMAGE_TAG)"

    is_safe_image_component "$previous_owner" || fail "The previous deployment image owner is invalid."
    is_safe_image_component "$WEKNORA_IMAGE_TAG" || fail "The previous deployment image tag is invalid."
    WEKNORA_IMAGE_OWNER="$previous_owner"
}

record_successful_deployment() {
    local temporary_state
    temporary_state="$(mktemp "${CI_DIRECTORY}/.current-successful.XXXXXX")"

    if [[ -f "$CURRENT_SUCCESS_PATH" ]]; then
        cp "$CURRENT_SUCCESS_PATH" "$PREVIOUS_SUCCESS_PATH"
    fi

    printf 'WEKNORA_IMAGE_OWNER=%s\nWEKNORA_IMAGE_TAG=%s\n' \
        "$WEKNORA_IMAGE_OWNER" "$WEKNORA_IMAGE_TAG" >"$temporary_state"
    mv "$temporary_state" "$CURRENT_SUCCESS_PATH"
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

case "$operation" in
    deploy)
        require_environment REQUESTED_IMAGE_TAG
        WEKNORA_IMAGE_TAG="$REQUESTED_IMAGE_TAG"
        ;;
    rollback)
        if [[ -n "${REQUESTED_IMAGE_TAG:-}" ]]; then
            WEKNORA_IMAGE_TAG="$REQUESTED_IMAGE_TAG"
        else
            load_previous_successful_tag
        fi
        ;;
esac

is_safe_image_component "$WEKNORA_IMAGE_TAG" || fail "WEKNORA_IMAGE_TAG is invalid."

IFS= read -r GHCR_PULL_TOKEN || fail "GHCR pull token was not received."
[[ -n "$GHCR_PULL_TOKEN" ]] || fail "GHCR pull token was empty."
DEPLOY_DOCKER_CONFIG="$(mktemp -d "${TMPDIR:-/tmp}/weknora-ghcr.XXXXXX")"
chmod 700 "$DEPLOY_DOCKER_CONFIG"
export DOCKER_CONFIG="$DEPLOY_DOCKER_CONFIG"
printf '%s' "$GHCR_PULL_TOKEN" | docker login ghcr.io --username "$GHCR_PULL_USERNAME" --password-stdin
unset GHCR_PULL_TOKEN

export WEKNORA_IMAGE_OWNER WEKNORA_IMAGE_TAG

echo "Validating Compose configuration for ${WEKNORA_IMAGE_OWNER}/weknora-*:${WEKNORA_IMAGE_TAG}."
compose config -q

echo "Pulling the requested application images."
compose pull "${SERVICES[@]}"

echo "Updating docreader and app without rebuilding or touching dependencies."
compose up -d --no-build --no-deps docreader app
wait_for_service_health docreader
wait_for_service_health app

echo "Updating frontend after the backend services are healthy."
compose up -d --no-build --no-deps frontend
wait_for_frontend_http

record_successful_deployment
echo "Deployment succeeded with image tag ${WEKNORA_IMAGE_TAG}."
