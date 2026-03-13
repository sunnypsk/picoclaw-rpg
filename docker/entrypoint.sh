#!/bin/sh
set -e

picoclaw_home() {
    if [ -n "${PICOCLAW_HOME:-}" ]; then
        printf '%s\n' "$PICOCLAW_HOME"
        return
    fi
    if [ -n "${PICOCLAW_CONFIG:-}" ]; then
        dirname "$PICOCLAW_CONFIG"
        return
    fi
    printf '%s/.picoclaw\n' "${HOME:-/home/picoclaw}"
}

seed_builtin_skills_if_empty() {
    workspace_dir="$1"
    skills_dir="$workspace_dir/skills"

    mkdir -p "$skills_dir"
    if [ -n "$(find "$skills_dir" -mindepth 1 -print -quit 2>/dev/null)" ]; then
        return
    fi

    tmp_root="$(mktemp -d)"
    tmp_home="$tmp_root/home"
    mkdir -p "$tmp_home"

    if HOME="$tmp_home" /usr/local/bin/picoclaw onboard >/dev/null 2>&1; then
        builtin_skills_dir="$tmp_home/.picoclaw/workspace/skills"
        if [ -n "$(find "$builtin_skills_dir" -mindepth 1 -print -quit 2>/dev/null)" ]; then
            cp -R "$builtin_skills_dir"/. "$skills_dir"/
            echo "Seeded builtin skills into $skills_dir"
        fi
    fi

    rm -rf "$tmp_root"
}

sync_managed_defaults() {
    if [ "${PICOCLAW_AUTO_SYNC_DEFAULTS:-1}" = "0" ]; then
        return
    fi

    if [ ! -f "$CONFIG_PATH" ]; then
        return
    fi

    if /usr/local/bin/picoclaw agent sync-defaults --force-legacy >/dev/null 2>&1; then
        echo "Synced managed workspace defaults"
    else
        echo "Warning: failed to sync managed workspace defaults" >&2
    fi
}

PICOCLAW_HOME_DIR="$(picoclaw_home)"
CONFIG_PATH="${PICOCLAW_CONFIG:-$PICOCLAW_HOME_DIR/config.json}"
WORKSPACE_DIR="${PICOCLAW_WORKSPACE:-$PICOCLAW_HOME_DIR/workspace}"

# First-run: neither config nor workspace exists.
# If config.json is already mounted but workspace is missing we skip onboard to
# avoid the interactive "Overwrite? (y/n)" prompt hanging in a non-TTY container.
if [ ! -d "$WORKSPACE_DIR" ] && [ ! -f "$CONFIG_PATH" ]; then
    /usr/local/bin/picoclaw onboard
    echo ""
    echo "First-run setup complete."
    echo "Edit $CONFIG_PATH (add your API key, etc.) then restart the container."
    exit 0
fi

seed_builtin_skills_if_empty "$WORKSPACE_DIR"
sync_managed_defaults

if [ "$#" -eq 0 ]; then
    set -- gateway
fi

exec /usr/local/bin/picoclaw "$@"
