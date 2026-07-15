#!/usr/bin/env bash
set -euo pipefail

REPO="Mapleeeeeeeeeee/cc-session-reader"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
SKILL_DIR="$HOME/.claude/skills/cc-session"
SKILL_URL="https://raw.githubusercontent.com/${REPO}/main/SKILL.md"
SKIP_SKILL=0

# ── arg parsing ──────────────────────────────────────────────────────────────

for arg in "$@"; do
  case "$arg" in
    --no-skill) SKIP_SKILL=1 ;;
    --help|-h)
      echo "Usage: install.sh [--no-skill]"
      echo "  --no-skill  Skip installing the Claude Code skill"
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

TMPDIR_CLEANUP=""
trap '[ -n "$TMPDIR_CLEANUP" ] && rm -rf "$TMPDIR_CLEANUP"' EXIT

install_binary() {
  local version="$1"
  local platform="$2"
  local version_bare="${version#v}"

  local download_url="https://github.com/${REPO}/releases/download/${version}/cc-session-reader_${version_bare}_${platform}.tar.gz"
  TMPDIR_CLEANUP=$(mktemp -d)

  echo "Downloading cc-session ${version} for ${platform}..."

  if command -v curl &>/dev/null; then
    curl -fsSL "$download_url" -o "$TMPDIR_CLEANUP/archive.tar.gz"
  else
    wget -qO "$TMPDIR_CLEANUP/archive.tar.gz" "$download_url"
  fi

  tar -xzf "$TMPDIR_CLEANUP/archive.tar.gz" -C "$TMPDIR_CLEANUP"

  mkdir -p "$INSTALL_DIR"
  mv "$TMPDIR_CLEANUP/cc-session" "$INSTALL_DIR/cc-session"
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
  if [ "$SKIP_SKILL" -eq 1 ]; then
    return
  fi

  if is_tty; then
    printf "Install Claude Code skill (cc-session)? [Y/n] "
    read -r answer
    if [[ "$answer" =~ ^[Nn]$ ]]; then
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

  sync_argument_hint

  echo "Skill installed. Use /cc-session in Claude Code to activate it."
}

# ── skill argument-hint sync ──────────────────────────────────────────────────

# sync_argument_hint overwrites the installed SKILL.md's "argument-hint:" line
# with the live output of "cc-session help --argument-hint". The CLI's command
# registry is the single source of truth for that hint; without this, the
# skill drifts out of sync whenever the CLI's subcommand order or set changes.
#
# Falls back to leaving the existing line untouched (no-op) if the binary
# isn't installed/executable, the subcommand errors out (e.g. an older CLI
# without "help --argument-hint"), or the output doesn't look like a hint —
# a broken skill install is worse than a stale hint.
sync_argument_hint() {
  local cli_bin="$INSTALL_DIR/cc-session"
  local skill_md="$SKILL_DIR/SKILL.md"

  if [ ! -x "$cli_bin" ]; then
    return
  fi

  local hint
  hint=$("$cli_bin" help --argument-hint 2>/dev/null) || return

  if [ -z "$hint" ] || [[ "$hint" != \[* ]]; then
    return
  fi

  # sed -i is not an option here: BSD sed (macOS) and GNU sed (Linux) take
  # incompatible flags for it. Writing to a temp file and moving it into
  # place works identically on both.
  local tmp_file
  tmp_file=$(mktemp "${skill_md}.XXXXXX")
  if awk -v hint="$hint" '
    /^argument-hint:/ { print "argument-hint: \"" hint "\""; next }
    { print }
  ' "$skill_md" > "$tmp_file"; then
    mv "$tmp_file" "$skill_md"
  else
    rm -f "$tmp_file"
  fi
}

# ── main ──────────────────────────────────────────────────────────────────────

print_next_steps() {
  echo ""
  echo "── Getting started ────────────────────────────────────────────────"
  echo "  cc-session list          # 列出最近的 session"
  echo "  cc-session read <id>     # 讀取對話內容"
  echo "  /cc-session              # 在 Claude Code 中使用 (需已安裝 Skill)"
  echo ""
  echo "── Token counting (optional) ──────────────────────────────────────"
  echo "  For precise token counts in 'cc-session stats', create:"
  echo "  $SKILL_DIR/config.json"
  echo ""
  echo '  {"anthropic_api_key_file": "<path-to-your-api-key-file>"}'
  echo ""
}

main() {
  local version platform
  version=$(fetch_latest_version)
  platform=$(detect_platform)

  install_binary "$version" "$platform"
  check_path
  install_skill
  print_next_steps
}

main
