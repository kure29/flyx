#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/current" "$fixture/legacy/kernels"
cat > "$fixture/agent" <<'EOF'
#!/bin/sh
echo image-binary
pwd
EOF
chmod +x "$fixture/agent"

run_agent() {
  docker run --rm \
    -v "$ROOT_DIR/go-gost/docker/entrypoint.sh:/entrypoint.sh:ro" \
    -v "$fixture/agent:/usr/local/bin/flyx-agent:ro" \
    -e FLYX_AGENT_IMAGE_VERSION="${image_version:-next}" \
    "$@" alpine:3.22 sh /entrypoint.sh
}

out=$(run_agent -v "$fixture/current:/etc/flyx-agent")
[[ "$out" == $'image-binary\n/etc/flyx-agent' ]]
[[ -x "$fixture/current/flyx-agent" ]]

# Old Compose files only mount the legacy directory, including on first use.
printf '{"secret":"keep"}\n' > "$fixture/legacy/config.json"
printf '{"epoch":"keep"}\n' > "$fixture/legacy/kernels/nft.json"
printf '{"pending":"keep"}\n' > "$fixture/legacy/traffic-queue.json"
out=$(run_agent -v "$fixture/legacy:/etc/flux_agent")
[[ "$out" == $'image-binary\n/etc/flux_agent' ]]
[[ -x "$fixture/legacy/flux_agent" ]]
[[ $(cat "$fixture/legacy/config.json") == '{"secret":"keep"}' ]]
[[ $(cat "$fixture/legacy/kernels/nft.json") == '{"epoch":"keep"}' ]]
[[ $(cat "$fixture/legacy/traffic-queue.json") == '{"pending":"keep"}' ]]

# An in-panel upgrade survives a restart of the same image.
# The binary belongs to container root; simulate the upgrade in the container
# so the host test runner can remain unprivileged.
docker run --rm -v "$fixture/legacy:/etc/flux_agent" alpine:3.22 \
  sh -c 'printf "#!/bin/sh\necho self-updated\n" > /etc/flux_agent/flux_agent'
out=$(run_agent -v "$fixture/legacy:/etc/flux_agent")
[[ "$out" == self-updated ]]
# Changing/rolling back the image version restores that image's binary.
image_version=previous
out=$(run_agent -v "$fixture/legacy:/etc/flux_agent")
[[ "$out" == $'image-binary\n/etc/flux_agent' ]]

printf '{}\n' > "$fixture/current/config.json"
if run_agent -v "$fixture/current:/etc/flyx-agent" -v "$fixture/legacy:/etc/flux_agent"; then
  echo 'ambiguous data directories were accepted' >&2
  exit 1
fi
echo 'Agent container identity, persistent state, restart and image rollback tests passed'
