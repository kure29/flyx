#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

load_script() {
  source <(sed '/^# 执行主函数$/,$d' "$ROOT_DIR/$1")
}

test_agent_installation_identity() (
  load_script install.sh
  root=$(mktemp -d)
  trap 'rm -rf "$root"' EXIT
  AGENT_ETC_ROOT="$root"
  SERVER_ADDR="panel.example.com:6365"
  resolve_agent_installation
  [[ "$INSTALL_DIR" == "$root/flyx-agent" && "$AGENT_NAME" == flyx-agent ]]
  # flux-panel / FLVX use the same directory; another panel's agent is left alone.
  mkdir -p "$root/flux_agent"
  printf '{\n  "addr": "other.example.com:6365",\n  "secret": "theirs"\n}\n' > "$root/flux_agent/config.json"
  touch "$root/flux_agent/flux_agent"
  resolve_agent_installation
  [[ "$INSTALL_DIR" == "$root/flyx-agent" && "$AGENT_NAME" == flyx-agent ]]
  SERVER_ADDR=
  resolve_agent_installation
  [[ "$INSTALL_DIR" == "$root/flyx-agent" ]]
  # A pre-3.4.0 FLYX agent of this panel keeps its path and config.
  SERVER_ADDR="panel.example.com:6365"
  printf '{"addr":"panel.example.com:6365","secret":"preserved"}\n' > "$root/flux_agent/config.json"
  resolve_agent_installation
  [[ "$INSTALL_DIR" == "$root/flux_agent" && "$AGENT_NAME" == flux_agent ]]
  [[ "$AGENT_SYSTEMD_SERVICE_FILE" == "$root/systemd/system/flux_agent.service" ]]
  [[ $(cat "$root/flux_agent/config.json") == '{"addr":"panel.example.com:6365","secret":"preserved"}' ]]
  # Once the new installation exists it is the one managed.
  mkdir -p "$root/flyx-agent"
  touch "$root/flyx-agent/config.json"
  resolve_agent_installation
  [[ "$INSTALL_DIR" == "$root/flyx-agent" ]]
  # The menu cannot compare addresses and asks before using the old path.
  rm -rf "$root/flyx-agent"
  SERVER_ADDR=
  resolve_agent_installation
  confirm_legacy_agent <<<'n' >/dev/null
  [[ "$INSTALL_DIR" == "$root/flyx-agent" ]]
  confirm_legacy_agent <<<'y' >/dev/null
  [[ "$INSTALL_DIR" == "$root/flux_agent" && "$AGENT_NAME" == flux_agent ]]
  # The original flux-panel gost service is not removed any more.
  ! declare -F cleanup_legacy_gost_installation >/dev/null
)

test_version_precedence() (
  for script in install.sh panel_install.sh; do
    load_script "$script"
    VERSION= FLYX_VERSION=new FLUX_VERSION=old
    [[ $(resolve_version) == new ]]
    unset FLYX_VERSION
    [[ $(resolve_version) == old ]]
  done
)

test_panel_new_install_defaults() (
  load_script panel_install.sh
  root=$(mktemp -d)
  trap 'rm -rf "$root"' EXIT
  cd "$root"
  printf 'JWT_SECRET=keep\n' > .env
  write_new_panel_identity
  [[ $(get_env_var PANEL_BACKEND_CONTAINER) == flyx-panel-backend ]]
  [[ $(get_env_var FLYX_SQLITE_VOLUME) == flyx-sqlite-data ]]
  [[ $(get_env_var FLYX_POSTGRES_VOLUME) == flyx-postgres-data ]]
  [[ $(get_env_var FLYX_NETWORK) == flyx-network ]]
  [[ $(get_env_var JWT_SECRET) == keep ]]
)

test_panel_legacy_detection() (
  load_script panel_install.sh
  root=$(mktemp -d)
  trap 'rm -rf "$root"' EXIT
  cd "$root"
  unset PANEL_BACKEND_CONTAINER PANEL_DEPLOY_DIR
  fake_image=ghcr.io/kure29/flux-panel-backend:3.3.0
  docker() {
    [[ "$4" == flux-panel-backend ]] || return 1
    if [[ "$3" == '{{.Config.Image}}' ]]; then printf '%s\n' "$fake_image"; else printf '/flux-panel-backend\n'; fi
  }
  [[ $(detect_panel_backend) == flux-panel-backend ]]
  # flux-panel / FLVX run their own flux-panel-backend; it is not this panel.
  fake_image=ghcr.io/sagit-chu/flux-panel-backend:2.0
  [[ $(detect_panel_backend) == flyx-panel-backend ]]
  # Explicit configuration selects the requested installation when both exist.
  fake_image=ghcr.io/kure29/flux-panel-backend:3.3.0
  docker() {
    if [[ "$3" == '{{.Config.Image}}' ]]; then printf '%s\n' "$fake_image"; else printf '/%s\n' "$4"; fi
  }
  if detect_panel_backend >/dev/null; then exit 1; fi
  printf 'PANEL_BACKEND_CONTAINER=custom-backend\n' > .env
  [[ $(detect_panel_backend) == custom-backend ]]
  # Another panel's deployment files are never taken for ours.
  printf 'services:\n  backend:\n    image: ghcr.io/sagit-chu/flux-panel-backend:2.0\n' > docker-compose.yml
  ! is_flyx_deploy_dir .
  # Installing a historical Compose release keeps its original resource names.
  printf 'services:\n  backend:\n    image: ghcr.io/kure29/flux-panel-backend:3.3.0\n' > docker-compose.yml
  is_flyx_deploy_dir .
  write_new_panel_identity
  [[ $(get_env_var FLYX_SQLITE_VOLUME) == sqlite_data ]]
  [[ $(get_env_var PANEL_BACKEND_CONTAINER) == flux-panel-backend ]]
)

test_panel_install_dir_and_ports() (
  load_script panel_install.sh
  root=$(mktemp -d)
  trap 'rm -rf "$root"' EXIT
  # A new install never writes into another panel's directory.
  mkdir -p "$root/other"
  printf 'JWT_SECRET=theirs\n' > "$root/other/.env"
  printf 'services:\n  backend:\n    image: ghcr.io/sagit-chu/flux-panel-backend:2.0\n' > "$root/other/docker-compose.yml"
  if PANEL_DEPLOY_DIR="$root/other" prepare_panel_install_dir >/dev/null 2>&1; then exit 1; fi
  [[ $(cat "$root/other/.env") == JWT_SECRET=theirs ]]
  PANEL_DEPLOY_DIR="$root/new" prepare_panel_install_dir >/dev/null
  [[ "$(pwd -P)" == "$(cd "$root/new" && pwd -P)" ]]
  # Ports taken by another panel are skipped.
  port_in_use() { [[ "$1" == 6366 || "$1" == 6365 ]]; }
  [[ $(next_free_port 6366) == 6368 ]]
  [[ $(next_free_port 6365 6367) == 6369 ]]
  [[ $(read_panel_port 前端端口 6368 <<<$'6366\n' 2>/dev/null) == 6368 ]]
)

test_agent_installation_identity
test_version_precedence
test_panel_new_install_defaults
test_panel_legacy_detection
test_panel_install_dir_and_ports
# Compose config renders locally; it does not contact Docker or start containers.
if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
  python3 - "$ROOT_DIR" <<'PYCOMPOSE'
import json, os, pathlib, subprocess, sys, tempfile
root=pathlib.Path(sys.argv[1])
env={k:v for k,v in os.environ.items() if not k.startswith(('FLYX_','FLUX_','FLVX_','PANEL_','POSTGRES_','COMPOSE_'))}
base='JWT_SECRET=test-only\nBACKEND_PORT=6365\nFRONTEND_PORT=6366\nFLUX_VERSION=old\nFLVX_WEBAUTHN_ORIGIN=https://panel.example.com\n'
with tempfile.TemporaryDirectory(prefix='flyx-compose-') as tmp:
 tmp=pathlib.Path(tmp)
 def render(path, values):
  e=tmp/'fixture.env';e.write_text(values)
  return json.loads(subprocess.check_output(['docker','compose','--env-file',str(e),'-f',str(path),'config','--format','json'],env=env,text=True))
 for name in ['docker-compose-v4.yml','docker-compose-v6.yml']:
  after=render(root/name,base)
  for service,container in {'backend':'flux-panel-backend','frontend':'vite-frontend','postgres':'flux-panel-postgres'}.items():
   assert after['services'][service]['container_name']==container
  assert after['volumes']['sqlite_data']['name']=='sqlite_data'
  assert after['volumes']['postgres_data']['name']=='postgres_data'
  assert [x['name'] for x in after['networks'].values()]==['gost-network']
  # Compose labels the network with this key; renaming it breaks upgrades.
  assert list(after['networks'])==['gost-network']
  assert after['services']['backend']['environment']['FLYX_WEBAUTHN_ORIGIN']=='https://panel.example.com'
  new=render(root/name,base+'FLYX_VERSION=new\nFLYX_WEBAUTHN_ORIGIN=\nPANEL_BACKEND_CONTAINER=flyx-panel-backend\nPANEL_FRONTEND_CONTAINER=flyx-panel-frontend\nPANEL_POSTGRES_CONTAINER=flyx-panel-postgres\nFLYX_SQLITE_VOLUME=flyx-sqlite-data\nFLYX_POSTGRES_VOLUME=flyx-postgres-data\nFLYX_NETWORK=flyx-network\n')
  assert new['services']['backend']['container_name']=='flyx-panel-backend'
  assert new['services']['backend']['image']=='ghcr.io/kure29/flyx-panel-backend:new'
  assert new['volumes']['sqlite_data']['name']=='flyx-sqlite-data'
  assert new['volumes']['postgres_data']['name']=='flyx-postgres-data'
  assert new['services']['backend']['environment']['FLYX_WEBAUTHN_ORIGIN']==''
print('IPv4/IPv6 Compose: old resources preserved; new namespace and env precedence passed')
PYCOMPOSE
fi
echo 'Project identity compatibility tests passed'
