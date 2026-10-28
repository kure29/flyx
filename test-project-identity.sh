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
  resolve_agent_installation
  [[ "$INSTALL_DIR" == "$root/flyx-agent" && "$AGENT_NAME" == flyx-agent ]]
  mkdir -p "$root/flux_agent"
  printf '{"secret":"preserved"}\n' > "$root/flux_agent/config.json"
  touch "$root/flux_agent/flux_agent"
  resolve_agent_installation
  [[ "$INSTALL_DIR" == "$root/flux_agent" && "$AGENT_NAME" == flux_agent ]]
  [[ $(cat "$root/flux_agent/config.json") == '{"secret":"preserved"}' ]]
  mkdir -p "$root/flyx-agent"
  touch "$root/flyx-agent/config.json"
  if resolve_agent_installation; then
    echo 'ambiguous installations must require explicit selection' >&2
    exit 1
  fi
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
  docker() {
    [[ "$4" == flux-panel-backend ]] || return 1
    printf '/flux-panel-backend\n'
  }
  [[ $(detect_panel_backend) == flux-panel-backend ]]
  # Explicit configuration selects the requested installation when both exist.
  docker() { printf '/%s\n' "$4"; }
  if detect_panel_backend >/dev/null; then exit 1; fi
  printf 'PANEL_BACKEND_CONTAINER=custom-backend\n' > .env
  [[ $(detect_panel_backend) == custom-backend ]]
  # Installing a historical Compose release keeps its original resource names.
  printf 'services: {}\n' > docker-compose.yml
  write_new_panel_identity
  [[ $(get_env_var FLYX_SQLITE_VOLUME) == sqlite_data ]]
  [[ $(get_env_var PANEL_BACKEND_CONTAINER) == flux-panel-backend ]]
)

test_agent_installation_identity
test_version_precedence
test_panel_new_install_defaults
test_panel_legacy_detection
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
