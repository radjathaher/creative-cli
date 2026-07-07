#!/usr/bin/env bash
# Install the `creative` binary from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/radjathaher/creative-cli/main/scripts/install.sh | bash
#
# Override the version with CREATIVE_CLI_VERSION=v0.1.0 and the install dir with
# CREATIVE_CLI_BIN_DIR=/usr/local/bin.
set -euo pipefail

REPO="radjathaher/creative-cli"
BIN="creative"

os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  arm64|aarch64) arch="aarch64" ;;
  x86_64|amd64)  arch="x86_64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

version="${CREATIVE_CLI_VERSION:-}"
if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -o '"tag_name": *"[^"]*"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
fi
if [ -z "$version" ]; then
  echo "could not resolve latest release tag" >&2; exit 1
fi
ver_no_v="${version#v}"

asset="creative-cli-${ver_no_v}-${os}-${arch}.tar.gz"
url="https://github.com/${REPO}/releases/download/${version}/${asset}"

bin_dir="${CREATIVE_CLI_BIN_DIR:-$HOME/.local/bin}"
if [ ! -d "$bin_dir" ]; then
  if [ -w /usr/local/bin ]; then bin_dir="/usr/local/bin"; else mkdir -p "$bin_dir"; fi
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "downloading $url" >&2
curl -fsSL "$url" -o "$tmp/pkg.tar.gz"
tar -xzf "$tmp/pkg.tar.gz" -C "$tmp"
install -m 0755 "$tmp/${BIN}" "$bin_dir/${BIN}"
echo "installed ${BIN} ${version} -> ${bin_dir}/${BIN}" >&2
