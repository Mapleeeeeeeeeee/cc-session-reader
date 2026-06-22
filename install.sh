#!/usr/bin/env bash
set -euo pipefail

REPO="Mapleeeeeeeeeee/cc-session-reader"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
SKILL_DIR="$HOME/.claude/skills/cc-session"
SKILL_URL="https://raw.githubusercontent.com/${REPO}/main/SKILL.md"
FORCE_SKILL=0

# ── arg parsing ──────────────────────────────────────────────────────────────

for arg in "$@"; do
  case "$arg" in
    --with-skill) FORCE_SKILL=1 ;;
    --help|-h)
      echo "Usage: install.sh [--with-skill]"
      echo "  --with-skill  Install the Claude Code skill without prompting"
      exit 0
      ;;
    *)
      echo "Unknown option: $arg" >&2
      exit 1
      ;;
  esac
done

# ── helpers ───────────────────────────────────────────────────────────────────

is_tty() { [ -t 0 ]; }

detect_shell_rc() {
  case "${SHELL:-}" in
    */zsh)  echo "$HOME/.zshrc" ;;
    */bash) echo "$HOME/.bashrc" ;;
    *)      echo "$HOME/.profile" ;;
  esac
}

# ── platform detection ────────────────────────────────────────────────────────

detect_platform() {
  local os arch

  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux)  os="linux" ;;
    *)
      echo "Unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac

  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac

  echo "${os}_${arch}"
}

# ── latest version lookup ─────────────────────────────────────────────────────

fetch_latest_version() {
  local api_url="https://api.github.com/repos/${REPO}/releases/latest"
  local version

  if command -v curl &>/dev/null; then
    version=$(curl -fsSL "$api_url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
  elif command -v wget &>/dev/null; then
    version=$(wget -qO- "$api_url" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
  else
    echo "Neither curl nor wget found. Please install one." >&2
    exit 1
  fi

  if [ -z "$version" ]; then
    echo "Failed to fetch latest release version." >&2
    exit 1
  fi

  echo "$version"
}

# ── binary download & install ─────────────────────────────────────────────────

install_binary() {
  local version="$1"
  local platform="$2"
  local version_bare="${version#v}"

  local download_url="https://github.com/${REPO}/releases/download/${version}/cc-session-reader_${version_bare}_${platform}.tar.gz"
  local tmpdir
  tmpdir=$(mktemp -d)
  trap 'rm -rf "$tmpdir"' EXIT

  echo "Downloading cc-session ${version} for ${platform}..."

  if command -v curl &>/dev/null; then
    curl -fsSL "$download_url" -o "$tmpdir/archive.tar.gz"
  else
    wget -qO "$tmpdir/archive.tar.gz" "$download_url"
  fi

  tar -xzf "$tmpdir/archive.tar.gz" -C "$tmpdir"

  mkdir -p "$INSTALL_DIR"
  mv "$tmpdir/cc-session" "$INSTALL_DIR/cc-session"
  chmod +x "$INSTALL_DIR/cc-session"

  echo "Installed cc-session to $INSTALL_DIR/cc-session"
}

# ── PATH check ────────────────────────────────────────────────────────────────

check_path() {
  if echo ":${PATH}:" | grep -q ":${INSTALL_DIR}:"; then
    return
  fi

  local export_line="export PATH=\"\$PATH:$INSTALL_DIR\""

  if ! is_tty; then
    echo ""
    echo "Warning: $INSTALL_DIR is not in your PATH."
    echo "Add it manually: $export_line"
    return
  fi

  echo ""
  echo "Warning: $INSTALL_DIR is not in your PATH."
  local shell_rc
  shell_rc=$(detect_shell_rc)
  printf "Add %s to PATH in %s? [y/N] " "$INSTALL_DIR" "$shell_rc"
  read -r answer
  if [[ "$answer" =~ ^[Yy]$ ]]; then
    echo "" >> "$shell_rc"
    echo "# Added by cc-session-reader installer" >> "$shell_rc"
    echo "$export_line" >> "$shell_rc"
    echo "Added to $shell_rc. Run: source $shell_rc"
  fi
}

# ── skill install ─────────────────────────────────────────────────────────────

install_skill() {
  if [ "$FORCE_SKILL" -eq 1 ]; then
    : # fall through to install
  elif ! is_tty; then
    echo "Non-interactive mode — use --with-skill to include skill install."
    return
  else
    printf "Install Claude Code skill (cc-session)? [y/N] "
    read -r answer
    if ! [[ "$answer" =~ ^[Yy]$ ]]; then
      return
    fi
  fi

  mkdir -p "$SKILL_DIR"

  echo "Installing Claude Code skill to $SKILL_DIR/SKILL.md..."

  if command -v curl &>/dev/null; then
    curl -fsSL "$SKILL_URL" -o "$SKILL_DIR/SKILL.md"
  else
    wget -qO "$SKILL_DIR/SKILL.md" "$SKILL_URL"
  fi

  echo "Skill installed. Use /cc-session in Claude Code to activate it."
}

# ── main ──────────────────────────────────────────────────────────────────────

main() {
  local version platform
  version=$(fetch_latest_version)
  platform=$(detect_platform)

  install_binary "$version" "$platform"
  check_path
  install_skill
}

main
