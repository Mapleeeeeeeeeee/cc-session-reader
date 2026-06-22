#!/usr/bin/env bash
# install.sh — installs the cc-session-reader CLI and optionally the Claude Code skill
set -euo pipefail

REPO="Mapleeeeeeeeeee/cc-session-reader"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
SKILL_DIR="$HOME/.claude/skills/cc-session"
SKILL_URL="https://raw.githubusercontent.com/${REPO}/main/skill/SKILL.md"

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    darwin) echo "darwin" ;;
    linux)  echo "linux" ;;
    *) echo "Unsupported OS: $os" >&2; exit 1 ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64)          echo "amd64" ;;
    aarch64|arm64)   echo "arm64" ;;
    *) echo "Unsupported arch: $arch" >&2; exit 1 ;;
  esac
}

fetch_latest_version() {
  if command -v gh &>/dev/null; then
    gh release view --repo "$REPO" --json tagName -q .tagName
  else
    curl -s "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep tag_name | cut -d'"' -f4
  fi
}

check_path() {
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) return 0 ;;
    *)
      echo "Warning: $INSTALL_DIR is not in PATH."
      echo "Add the following to your ~/.zshrc or ~/.bashrc:"
      echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
      ;;
  esac
}

install_skill() {
  if [ ! -t 0 ]; then
    echo "Non-interactive mode — skipping skill install."
    return
  fi

  printf "Install Claude Code skill to %s? [y/N] " "$SKILL_DIR"
  read -r answer
  case "$answer" in
    [yY]|[yY][eE][sS]) ;;
    *) echo "Skipping skill install."; return ;;
  esac

  mkdir -p "$SKILL_DIR"
  curl -fsSL "$SKILL_URL" -o "$SKILL_DIR/SKILL.md"
  echo "Skill installed to $SKILL_DIR/SKILL.md"
}

main() {
  local os arch version version_bare download_url tmpdir

  os="$(detect_os)"
  arch="$(detect_arch)"

  echo "Fetching latest release..."
  version="$(fetch_latest_version)"

  if [ -z "$version" ]; then
    echo "Error: could not determine latest version." >&2
    exit 1
  fi

  version_bare="${version#v}"
  download_url="https://github.com/${REPO}/releases/download/${version}/cc-session-reader_${version_bare}_${os}_${arch}.tar.gz"

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  echo "Downloading cc-session-reader ${version} (${os}/${arch})..."
  curl -fsSL "$download_url" -o "$tmpdir/cc-session-reader.tar.gz"

  tar -xzf "$tmpdir/cc-session-reader.tar.gz" -C "$tmpdir"

  mkdir -p "$INSTALL_DIR"
  mv "$tmpdir/cc-session" "$INSTALL_DIR/cc-session"
  chmod +x "$INSTALL_DIR/cc-session"

  check_path

  echo "Successfully installed cc-session-reader ${version} to $INSTALL_DIR/cc-session"

  install_skill
}

main "$@"
