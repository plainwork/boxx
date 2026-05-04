#!/usr/bin/env sh
# boxx installer — bootstraps Docker if needed, then installs the boxx binary.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/plainwork/boxx/main/install.sh | sh
#
# Options (env vars):
#   PREFIX=<dir>      install boxx here (default: /usr/local/bin)
#   VERSION=<tag>     pin a specific release (default: latest)
#   SKIP_DOCKER=1     skip Docker install/setup

set -eu

REPO="plainwork/boxx"
PREFIX="${PREFIX:-/usr/local/bin}"
VERSION="${VERSION:-latest}"
SKIP_DOCKER="${SKIP_DOCKER:-0}"

# ── helpers ──────────────────────────────────────────────────────────────────

say()  { printf '\033[1mboxx:\033[0m %s\n' "$*"; }
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; }
info() { printf '  \033[2m→\033[0m %s\n' "$*"; }
die()  { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "required tool not found: $1"
}

uname_os() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    darwin|linux) printf '%s' "$os" ;;
    *) die "unsupported OS: $os" ;;
  esac
}

uname_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)   printf 'amd64' ;;
    aarch64|arm64)  printf 'arm64' ;;
    armv7l|armv7)   printf 'armv7' ;;
    *) die "unsupported architecture: $arch" ;;
  esac
}

need curl
need tar

OS="$(uname_os)"
ARCH="$(uname_arch)"

# ── Docker bootstrap (Linux only) ─────────────────────────────────────────────

if [ "$OS" = "linux" ] && [ "$SKIP_DOCKER" = "0" ]; then
  if command -v docker >/dev/null 2>&1; then
    ok "Docker already installed ($(docker --version 2>/dev/null | head -1))"
  else
    say "Docker not found — installing via get.docker.com …"
    curl -fsSL https://get.docker.com | sh
    ok "Docker installed"
  fi

  # Enable and start Docker via systemd (idempotent)
  if command -v systemctl >/dev/null 2>&1; then
    systemctl is-enabled docker >/dev/null 2>&1 || {
      info "enabling Docker service …"
      sudo systemctl enable --now docker
    }
    systemctl is-active docker >/dev/null 2>&1 || {
      info "starting Docker service …"
      sudo systemctl start docker
    }
    ok "Docker service enabled and running"
  fi

  # Add current user to the docker group so sudo isn't needed for docker commands
  CURRENT_USER="${SUDO_USER:-$(id -un)}"
  if [ -n "$CURRENT_USER" ] && [ "$CURRENT_USER" != "root" ]; then
    if ! groups "$CURRENT_USER" 2>/dev/null | grep -q '\bdocker\b'; then
      info "adding $CURRENT_USER to docker group …"
      sudo usermod -aG docker "$CURRENT_USER"
      ok "added $CURRENT_USER to docker group (re-login to apply)"
    else
      ok "$CURRENT_USER is already in the docker group"
    fi
  fi
fi

# ── Resolve version ───────────────────────────────────────────────────────────

if [ "$VERSION" = "latest" ]; then
  say "Resolving latest release …"
  TAG="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  [ -n "$TAG" ] || die "could not determine latest release — check https://github.com/$REPO/releases"
else
  TAG="$VERSION"
fi

# ── Download and install boxx binary ─────────────────────────────────────────

# GoReleaser names: boxx_1.2.3_linux_amd64.tar.gz  (arm → armv7)
ARCH_SUFFIX="$ARCH"
[ "$ARCH_SUFFIX" = "armv7" ] && ARCH_SUFFIX="armv7"

ARCHIVE="boxx_${TAG#v}_${OS}_${ARCH_SUFFIX}.tar.gz"
URL="https://github.com/$REPO/releases/download/$TAG/$ARCHIVE"

say "Downloading boxx $TAG ($OS/$ARCH) …"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" -o "$TMP/boxx.tgz" || die "download failed: $URL"
tar -xzf "$TMP/boxx.tgz" -C "$TMP"

if [ -w "$PREFIX" ]; then
  install -m 0755 "$TMP/boxx" "$PREFIX/boxx"
else
  sudo install -m 0755 "$TMP/boxx" "$PREFIX/boxx"
fi

ok "boxx $TAG installed to $PREFIX/boxx"

# ── Done ──────────────────────────────────────────────────────────────────────

echo
"$PREFIX/boxx" doctor || true
echo
say "Ready. Run \`boxx\` for the TUI or \`boxx install <image> --host <hostname>\` to deploy your first app."
if [ "$OS" = "linux" ]; then
  echo
  info "Tip: if this is a fresh login, run \`newgrp docker\` (or re-login) to use Docker without sudo."
  echo
  PUBLIC_IP="$(curl -fsSL --max-time 5 https://api.ipify.org 2>/dev/null || true)"
  if [ -n "$PUBLIC_IP" ]; then
    say "This server's public IP is: \033[1m$PUBLIC_IP\033[0m"
    info "Point your domain's A record to $PUBLIC_IP, then run:"
    info "  boxx install <image> --host <yourdomain.com>"
  fi
fi
