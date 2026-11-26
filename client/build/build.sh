#!/usr/bin/env bash
# 按 client/upstream.lock 拉取 CrossDesk 上游源码，应用 client/patches/ 后可选构建。
#
# 用法：
#   ./client/build/build.sh --dry-run   只打印计划，不下载
#   ./client/build/build.sh             拉源码 + 打 patch
#   ./client/build/build.sh --build     拉源码 + 打 patch + xmake 构建
#
# 依赖：git；--build 时还需要 xmake 与对应平台编译工具链。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CLIENT_DIR="$ROOT/client"
LOCK="$CLIENT_DIR/upstream.lock"
PATCH_DIR="$CLIENT_DIR/patches"
SRC_DIR="$CLIENT_DIR/upstream"

DRY_RUN=0
BUILD=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --build) BUILD=1 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

[ -f "$LOCK" ] || { echo "missing $LOCK" >&2; exit 1; }

get_field() {
  awk -F= -v key="$1" '$1==key { sub(/^[^=]*=/, ""); print; exit }' "$LOCK"
}

UPSTREAM_URL="$(get_field url)"
REF="$(get_field ref)"
COMMIT="$(get_field commit)"

[ -n "$UPSTREAM_URL" ] || { echo "upstream.lock: url is empty" >&2; exit 1; }
if [ -z "$REF" ] && [ -z "$COMMIT" ]; then
  echo "upstream.lock: ref and commit are both empty" >&2
  exit 1
fi

shopt -s nullglob
PATCHES=("$PATCH_DIR"/*.patch)

echo "== bnyc-remote client build =="
echo "upstream : $UPSTREAM_URL"
echo "ref      : ${REF:-<none>}"
echo "commit   : ${COMMIT:-<none, use ref>}"
echo "patches  : ${#PATCHES[@]}"
echo "source   : $SRC_DIR"

if [ "$DRY_RUN" = "1" ]; then
  echo "[dry-run] would clone upstream and apply the patches above"
  exit 0
fi

rm -rf "$SRC_DIR"
if [ -n "$COMMIT" ]; then
  git clone "$UPSTREAM_URL" "$SRC_DIR"
  git -C "$SRC_DIR" checkout "$COMMIT"
else
  git clone --depth 1 --branch "$REF" "$UPSTREAM_URL" "$SRC_DIR"
fi

# 上游依赖 submodule（deps/、libs/minirtc 等），构建前必须初始化。
echo "== submodules =="
git -C "$SRC_DIR" submodule update --init --recursive

if [ ${#PATCHES[@]} -gt 0 ]; then
  echo "== apply patches =="
  for p in "${PATCHES[@]}"; do
    echo "-> $(basename "$p")"
    git -C "$SRC_DIR" apply --check "$p"
    git -C "$SRC_DIR" apply "$p"
  done
else
  echo "== no patches to apply =="
fi

if [ "$BUILD" = "1" ]; then
  command -v xmake >/dev/null 2>&1 || { echo "xmake not found" >&2; exit 1; }
  echo "== xmake build =="
  ( cd "$SRC_DIR" && xmake b -vy crossdesk )
  echo "构建完成，产物见上游仓库 build/ 或 target/ 目录"
else
  echo "源码已就绪：$SRC_DIR"
  echo "如需构建：$0 --build"
fi
