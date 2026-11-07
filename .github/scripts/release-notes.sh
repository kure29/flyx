#!/usr/bin/env bash
# Prints the GitHub Release notes for a version: its section of
# doc/changelog.md (or, when it has none, the commits since the previous
# release), the images and the install commands.
#
# Usage: release-notes.sh VERSION REPO IMAGE_PREFIX [SHA]
#   REPO          owner/name, e.g. kure29/flyx
#   IMAGE_PREFIX  registry/owner, e.g. ghcr.io/kure29
#   SHA           the commit being released (default HEAD)
set -euo pipefail

VERSION="$1"
REPO="$2"
IMAGE_PREFIX="$3"
SHA="${4:-HEAD}"
CHANGELOG="${CHANGELOG:-doc/changelog.md}"

# The version's section: from "## VERSION" up to the next "## " heading,
# without the heading and the date line under it.
changes=""
if [ -f "$CHANGELOG" ]; then
  changes=$(awk -v v="$VERSION" '
    /^## / { if (found) exit; if ($2 == v) { found = 1; next } }
    found && !started && /^[0-9]{4}-[0-9]{2}-[0-9]{2}$/ { next }
    found && /[^[:space:]]/ { started = 1 }
    found && started { print }
  ' "$CHANGELOG")
  # Changelog links are relative to the docs; point them at the site.
  changes=$(printf '%s\n' "$changes" | sed -E \
    -e "s#\]\(\./guide/([^)#]+)\.md#](https://${REPO%%/*}.github.io/${REPO#*/}/guide/\1#g" \
    -e "s#\]\(\./([^)#]+)\.md#](https://${REPO%%/*}.github.io/${REPO#*/}/\1#g")
fi

prev=$(git describe --tags --abbrev=0 --exclude "$VERSION" "$SHA" 2>/dev/null || true)

if [ -z "$(printf '%s' "$changes" | tr -d '[:space:]')" ]; then
  range="$SHA"
  [ -n "$prev" ] && range="$prev..$SHA"
  changes=$(git log --no-merges -n 50 --format='- %s' "$range")
  [ -n "$changes" ] || changes="- 没有新的提交"
fi

echo "## 更新内容"
echo
printf '%s\n' "$changes"
echo
if [ -n "$prev" ]; then
  echo "完整改动：[\`$prev...$VERSION\`](https://github.com/$REPO/compare/$prev...$VERSION) · [更新日志](https://${REPO%%/*}.github.io/${REPO#*/}/changelog)"
  echo
fi

cat <<EOF
## 📦 Docker Images

\`\`\`bash
# Backend
docker pull $IMAGE_PREFIX/flyx-panel-backend:$VERSION

# Frontend
docker pull $IMAGE_PREFIX/flyx-panel-frontend:$VERSION
\`\`\`

## 🚀 Quick Install

**Panel (安装此版本 $VERSION):**
\`\`\`bash
curl -L https://github.com/$REPO/releases/download/$VERSION/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
\`\`\`

**Node (安装此版本 $VERSION):**
\`\`\`bash
curl -L https://github.com/$REPO/releases/download/$VERSION/install.sh -o install.sh && chmod +x install.sh && ./install.sh
\`\`\`

**安装最新版:**
\`\`\`bash
# 面板端
curl -L https://github.com/$REPO/releases/latest/download/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
# 节点端
curl -L https://github.com/$REPO/releases/latest/download/install.sh -o install.sh && chmod +x install.sh && ./install.sh
\`\`\`
EOF
