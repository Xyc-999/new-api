#!/usr/bin/env bash
set -euo pipefail

# One-shot Ubuntu host bootstrap for this new-api project.
# It installs system dependencies and initializes nginx/SSL only.
# It does not create .env files and does not run docker compose.

# Set to false if another reverse proxy already handles nginx and HTTPS.
ENABLE_NGINX_SSL="${ENABLE_NGINX_SSL:-true}"

SCRIPT_NAME="$(basename "$0")"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUDO=""
if [ "${EUID}" -ne 0 ]; then
  SUDO="sudo"
fi

log() {
  printf '[%s] %s\n' "$SCRIPT_NAME" "$*"
}

fail() {
  printf '[%s] ERROR: %s\n' "$SCRIPT_NAME" "$*" >&2
  exit 1
}

ensure_ubuntu() {
  if ! command -v apt-get >/dev/null 2>&1; then
    fail "This script targets Ubuntu/Debian servers with apt-get."
  fi
}

install_base_packages() {
  log "Installing base packages..."
  ${SUDO} apt-get update
  ${SUDO} env DEBIAN_FRONTEND=noninteractive apt-get install -y \
    ca-certificates \
    curl \
    git \
    gnupg \
    lsb-release
}

install_docker() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    log "Docker and docker compose plugin already installed."
    return
  fi

  log "Installing Docker Engine from Docker apt repository..."
  ${SUDO} install -m 0755 -d /etc/apt/keyrings
  if [ ! -f /etc/apt/keyrings/docker.gpg ]; then
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | ${SUDO} gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    ${SUDO} chmod a+r /etc/apt/keyrings/docker.gpg
  fi

  local arch
  local codename
  arch="$(dpkg --print-architecture)"
  codename="$(
    . /etc/os-release
    printf '%s' "${VERSION_CODENAME:-}"
  )"
  if [ -z "$codename" ]; then
    codename="$(lsb_release -cs)"
  fi

  printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu %s stable\n' "$arch" "$codename" \
    | ${SUDO} tee /etc/apt/sources.list.d/docker.list >/dev/null

  ${SUDO} apt-get update
  ${SUDO} env DEBIAN_FRONTEND=noninteractive apt-get install -y \
    docker-ce \
    docker-ce-cli \
    containerd.io \
    docker-buildx-plugin \
    docker-compose-plugin

  if command -v systemctl >/dev/null 2>&1; then
    ${SUDO} systemctl enable --now docker
  fi

  if [ -n "${SUDO_USER:-}" ] && [ "${SUDO_USER}" != "root" ]; then
    ${SUDO} usermod -aG docker "$SUDO_USER" || true
    log "User ${SUDO_USER} was added to the docker group. Re-login later to use docker without sudo."
  fi
}

setup_nginx_ssl() {
  if [ "$ENABLE_NGINX_SSL" != "true" ]; then
    log "ENABLE_NGINX_SSL=false, skipping nginx and SSL setup."
    return
  fi

  log "Configuring nginx reverse proxy and SSL..."
  bash "${SCRIPT_DIR}/setup-nginx-ssl.sh"
}

main() {
  ensure_ubuntu
  install_base_packages
  install_docker
  setup_nginx_ssl

  log "Host bootstrap finished."
  log "Next step: configure your .env file and run docker compose manually."
}

main "$@"
