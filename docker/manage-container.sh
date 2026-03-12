#!/usr/bin/env bash
set -euo pipefail

ACTION="${1:-menu}"
CONTAINER_NAME="picoclaw-gateway"
IMAGE_NAME="${PICOCLAW_IMAGE:-ghcr.io/sunnypsk/picoclaw-rpg:main}"
USE_ROOT=0
FORCE=0

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
DOCKERFILE_PATH="$SCRIPT_DIR/Dockerfile"
DATA_DIR="$SCRIPT_DIR/data"

usage() {
  cat <<'EOF'
Usage:
  ./docker/manage-container.sh [action] [--root] [--force]

Actions:
  menu       Show interactive menu (default)
  status     Show container status
  build      Build image
  run        Run gateway container
  start      Start container
  stop       Stop container
  restart    Restart container
  remove     Remove container
  logs       Tail container logs
  recreate   Remove and run container again

Options:
  --root     Run container as root and mount to /root/.picoclaw
  --force    Skip remove confirmation
  -h, --help Show this help
EOF
}

section() {
  printf '\n== %s ==\n' "$1"
}

docker_cmd() {
  printf '> docker %s\n' "$*"
  docker "$@"
}

require_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    echo 'Docker CLI is not installed or not in PATH.' >&2
    exit 1
  fi
}

container_home() {
  if [[ "$USE_ROOT" -eq 1 ]]; then
    printf '/root/.picoclaw\n'
  else
    printf '/home/picoclaw/.picoclaw\n'
  fi
}

container_user() {
  if [[ "$USE_ROOT" -eq 1 ]]; then
    printf 'root\n'
  else
    printf '\n'
  fi
}

container_record() {
  docker ps -a --filter "name=^/${CONTAINER_NAME}$" --format '{{.ID}}|{{.Status}}|{{.Names}}'
}

container_state() {
  local record
  record="$(container_record)"
  if [[ -z "$record" ]]; then
    printf 'missing\n'
  elif [[ "$record" == *'|Up '* ]]; then
    printf 'running\n'
  else
    printf 'stopped\n'
  fi
}

ensure_data_dir() {
  mkdir -p "$DATA_DIR"
}

show_status() {
  section 'Container Status'
  local state record status_line user mount
  state="$(container_state)"
  record="$(container_record)"
  user="$(container_user)"
  mount="$(container_home)"

  printf 'Container: %s\n' "$CONTAINER_NAME"
  printf 'Image:     %s\n' "$IMAGE_NAME"
  printf 'Data dir:  %s\n' "$DATA_DIR"
  printf 'Mount:     %s\n' "$mount"
  if [[ -n "$user" ]]; then
    printf 'User:      %s\n' "$user"
  else
    printf 'User:      image default\n'
  fi
  printf 'State:     %s\n' "$state"

  if [[ -n "$record" ]]; then
    status_line="${record#*|}"
    status_line="${status_line%|*}"
    printf 'Status:    %s\n' "$status_line"
  fi
}

build_image() {
  section 'Build Image'
  docker_cmd build -t "$IMAGE_NAME" -f "$DOCKERFILE_PATH" "$REPO_ROOT"
}

run_container() {
  section 'Run Container'
  ensure_data_dir

  local state mount user
  state="$(container_state)"
  if [[ "$state" == 'running' ]]; then
    echo "$CONTAINER_NAME is already running."
    return
  fi
  if [[ "$state" == 'stopped' ]]; then
    echo "$CONTAINER_NAME already exists but is stopped. Starting it instead."
    start_container
    return
  fi

  mount="$(container_home)"
  user="$(container_user)"

  if [[ -n "$user" ]]; then
    docker_cmd run -d --name "$CONTAINER_NAME" --restart on-failure --user "$user" -v "$DATA_DIR:$mount" "$IMAGE_NAME" gateway
  else
    docker_cmd run -d --name "$CONTAINER_NAME" --restart on-failure -v "$DATA_DIR:$mount" "$IMAGE_NAME" gateway
  fi
}

start_container() {
  section 'Start Container'
  local state
  state="$(container_state)"
  if [[ "$state" == 'missing' ]]; then
    echo "$CONTAINER_NAME does not exist yet. Use Run first."
    return
  fi
  if [[ "$state" == 'running' ]]; then
    echo "$CONTAINER_NAME is already running."
    return
  fi
  docker_cmd start "$CONTAINER_NAME"
}

stop_container() {
  section 'Stop Container'
  local state
  state="$(container_state)"
  if [[ "$state" == 'missing' ]]; then
    echo "$CONTAINER_NAME does not exist."
    return
  fi
  if [[ "$state" == 'stopped' ]]; then
    echo "$CONTAINER_NAME is already stopped."
    return
  fi
  docker_cmd stop "$CONTAINER_NAME"
}

restart_container() {
  section 'Restart Container'
  local state
  state="$(container_state)"
  if [[ "$state" == 'missing' ]]; then
    echo "$CONTAINER_NAME does not exist yet. Running a new container instead."
    run_container
    return
  fi
  docker_cmd restart "$CONTAINER_NAME"
}

remove_container() {
  section 'Remove Container'
  local state confirm
  state="$(container_state)"
  if [[ "$state" == 'missing' ]]; then
    echo "$CONTAINER_NAME does not exist."
    return
  fi

  if [[ "$FORCE" -ne 1 ]]; then
    read -r -p "Remove $CONTAINER_NAME? This keeps docker/data but deletes the container. [y/N] " confirm
    if [[ ! "$confirm" =~ ^([yY]|yes|YES)$ ]]; then
      echo 'Cancelled.'
      return
    fi
  fi

  docker_cmd rm -f "$CONTAINER_NAME"
}

show_logs() {
  section 'Container Logs'
  if [[ "$(container_state)" == 'missing' ]]; then
    echo "$CONTAINER_NAME does not exist."
    return
  fi
  docker_cmd logs -f "$CONTAINER_NAME"
}

recreate_container() {
  section 'Recreate Container'
  if [[ -n "$(container_record)" ]]; then
    docker_cmd rm -f "$CONTAINER_NAME"
  fi
  run_container
}

show_menu() {
  while true; do
    section 'PicoClaw Container Manager'
    echo '1. Status'
    echo '2. Build image'
    echo '3. Run gateway container'
    echo '4. Start container'
    echo '5. Stop container'
    echo '6. Restart container'
    echo '7. Show logs'
    echo '8. Remove container'
    echo '9. Rebuild and recreate'
    echo '0. Exit'
    read -r -p 'Choose an action: ' choice

    case "$choice" in
      1) show_status ;;
      2) build_image ;;
      3) run_container ;;
      4) start_container ;;
      5) stop_container ;;
      6) restart_container ;;
      7) show_logs ;;
      8) remove_container ;;
      9) build_image; recreate_container ;;
      0) return ;;
      *) echo 'Unknown choice.' ;;
    esac
  done
}

parse_args() {
  local positional=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      menu|status|build|run|start|stop|restart|remove|logs|recreate)
        ACTION="$1"
        shift
        ;;
      --root)
        USE_ROOT=1
        shift
        ;;
      --force)
        FORCE=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        positional+=("$1")
        shift
        ;;
    esac
  done

  if [[ ${#positional[@]} -gt 0 ]]; then
    echo "Unknown arguments: ${positional[*]}" >&2
    usage
    exit 1
  fi
}

require_docker
parse_args "$@"

case "$ACTION" in
  menu) show_menu ;;
  status) show_status ;;
  build) build_image ;;
  run) run_container ;;
  start) start_container ;;
  stop) stop_container ;;
  restart) restart_container ;;
  remove) remove_container ;;
  logs) show_logs ;;
  recreate) recreate_container ;;
  *)
    echo "Unknown action: $ACTION" >&2
    usage
    exit 1
    ;;
esac
