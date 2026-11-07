#!/bin/sh
# Runs the agent from the data volume so the panel's one-click upgrade
# survives restarts. A newly pulled image (different version) puts its own
# binary back in place.
set -e

# Old Compose files bind-mount /etc/flux_agent. Keep that mount and binary
# name so existing state and old in-panel upgraders remain usable.
DATA_DIR=${FLYX_AGENT_DATA_DIR:-/etc/flyx-agent}
if [ -z "${FLYX_AGENT_DATA_DIR:-}" ] && [ -d /etc/flux_agent ]; then
  if [ -f /etc/flyx-agent/config.json ]; then
    echo "Both agent data directories exist; set FLYX_AGENT_DATA_DIR explicitly" >&2
    exit 1
  fi
  DATA_DIR=/etc/flux_agent
fi
AGENT_NAME=flyx-agent
if [ "$DATA_DIR" = /etc/flux_agent ]; then AGENT_NAME=flux_agent; fi
BIN="$DATA_DIR/$AGENT_NAME"
IMAGE_VERSION=${FLYX_AGENT_IMAGE_VERSION-${FLUX_AGENT_IMAGE_VERSION:-dev}}
MARK="$DATA_DIR/.image_version"

mkdir -p "$DATA_DIR"
cd "$DATA_DIR"

if [ ! -x "$BIN" ] || [ "$(cat "$MARK" 2>/dev/null)" != "$IMAGE_VERSION" ]; then
  cp /usr/local/bin/flyx-agent "$BIN.new"
  chmod 755 "$BIN.new"
  mv "$BIN.new" "$BIN"
  echo "$IMAGE_VERSION" > "$MARK"
fi

[ -f gost.json ] || echo '{}' > gost.json
chmod 600 gost.json

# Crashing helpers (nft, kernels) would otherwise write core files into the
# data directory on hosts with a relative core_pattern.
ulimit -c 0

exec "$BIN" "$@"
