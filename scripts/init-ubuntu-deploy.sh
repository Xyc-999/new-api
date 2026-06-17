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
  echo "==================== 使用阿里云国内源安装Docker ===================="

  # 1. 清理旧版docker
  echo "卸载系统旧Docker组件"
  apt remove -y docker docker-engine docker.io containerd runc || true

  # 2. 安装依赖
  echo "[1/5] 安装依赖 ca-certificates curl gnupg lsb-release"
  apt update
  apt install -y ca-certificates curl gnupg lsb-release

  # 3. 导入阿里云Docker GPG密钥（公网地址mirrors.aliyun.com）
  echo "[2/5] 拉取阿里云Docker签名密钥"
  install -m 0755 -d /etc/apt/keyrings
  # 阿里云公网GPG地址
  curl -fsSL https://mirrors.aliyun.com/docker-ce/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
  chmod a+r /etc/apt/keyrings/docker.gpg

  # 4. 添加阿里云Docker CE软件源
  echo "[3/5] 写入阿里云Docker apt源"
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://mirrors.aliyun.com/docker-ce/linux/ubuntu $(lsb_release -cs) stable" | tee /etc/apt/sources.list.d/docker.list > /dev/null

  # 5. 更新源并安装全套docker
  echo "[4/5] 安装 docker-ce + compose插件"
  apt update
  apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

  # 6. 配置国内镜像加速器（阿里云+多备用镜像）
  echo "[5/5] 配置容器镜像加速"
  tee /etc/docker/daemon.json <<-'EOF'
  {
    "registry-mirrors": [
      "https://docker.1ms.run",
      "https://hub-mirror.c.163.com"
    ],
    "features": {
      "buildkit": true
    },
    "log-driver": "json-file",
    "log-opts": {
      "max-size": "10m"
    }
  }
EOF

  # 重载配置、重启、开机自启
  systemctl daemon-reload
  systemctl restart docker
  systemctl enable --now docker

  # 输出校验信息
  echo -e "\n==================== 安装完成 ===================="
  docker --version
  docker compose version
  echo -e "\n测试拉取hello-world验证："
  docker run --rm hello-world

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
