#!/usr/bin/env bash
set -euo pipefail

# Reusable nginx + Let's Encrypt setup for a single reverse-proxy app.
# Copy this file to another project and adjust only the variables below.

DOMAIN="${DOMAIN:-api.example.com}"
EXTRA_DOMAINS="${EXTRA_DOMAINS:-}"
LETSENCRYPT_EMAIL="${LETSENCRYPT_EMAIL:-admin@example.com}"
SITE_NAME="${SITE_NAME:-new-api}"
UPSTREAM_HOST="${UPSTREAM_HOST:-127.0.0.1}"
UPSTREAM_PORT="${UPSTREAM_PORT:-3000}"
CLIENT_MAX_BODY_SIZE="${CLIENT_MAX_BODY_SIZE:-50m}"
PROXY_READ_TIMEOUT="${PROXY_READ_TIMEOUT:-600s}"
CERTBOT_STAGING="${CERTBOT_STAGING:-false}"
ACME_WEBROOT="${ACME_WEBROOT:-/var/www/certbot}"

# Set to false only when you want nginx HTTP reverse proxy without TLS.
ENABLE_SSL="${ENABLE_SSL:-true}"

SCRIPT_NAME="$(basename "$0")"
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

require_value() {
  local value="$1"
  local name="$2"
  if [ -z "$value" ] || [ "$value" = "api.example.com" ] || [ "$value" = "admin@example.com" ]; then
    fail "Please configure ${name} at the top of this script or pass it as an environment variable."
  fi
}

install_packages() {
  if ! command -v apt-get >/dev/null 2>&1; then
    fail "This script targets Ubuntu/Debian servers with apt-get."
  fi

  log "Installing nginx and certbot packages..."
  ${SUDO} apt-get update
  ${SUDO} env DEBIAN_FRONTEND=noninteractive apt-get install -y \
    nginx \
    certbot
}

server_names() {
  local names="$DOMAIN"
  if [ -n "$EXTRA_DOMAINS" ]; then
    names="$names $(printf '%s' "$EXTRA_DOMAINS" | tr ',' ' ')"
  fi
  printf '%s' "$names"
}

certbot_domain_args() {
  printf -- '-d %s ' "$DOMAIN"
  if [ -n "$EXTRA_DOMAINS" ]; then
    local domain
    printf '%s' "$EXTRA_DOMAINS" | tr ',' '\n' | while IFS= read -r domain; do
      domain="$(printf '%s' "$domain" | xargs)"
      if [ -n "$domain" ]; then
        printf -- '-d %s ' "$domain"
      fi
    done
  fi
}

write_http_proxy_config() {
  local tmp_file
  local server_name_line
  tmp_file="$(mktemp)"
  server_name_line="$(server_names)"

  log "Writing HTTP nginx site config for ${server_name_line} -> ${UPSTREAM_HOST}:${UPSTREAM_PORT}..."
  cat >"$tmp_file" <<NGINX
server {
    listen 80;
    listen [::]:80;
    server_name ${server_name_line};

    client_max_body_size ${CLIENT_MAX_BODY_SIZE};

    location / {
        proxy_pass http://${UPSTREAM_HOST}:${UPSTREAM_PORT};
        proxy_http_version 1.1;

        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_connect_timeout 60s;
        proxy_send_timeout ${PROXY_READ_TIMEOUT};
        proxy_read_timeout ${PROXY_READ_TIMEOUT};
    }
}
NGINX

  install_nginx_config "$tmp_file"
}

write_acme_http_config() {
  local tmp_file
  local server_name_line
  tmp_file="$(mktemp)"
  server_name_line="$(server_names)"

  log "Writing temporary HTTP config for ACME challenge..."
  ${SUDO} mkdir -p "$ACME_WEBROOT"
  cat >"$tmp_file" <<NGINX
server {
    listen 80;
    listen [::]:80;
    server_name ${server_name_line};

    location /.well-known/acme-challenge/ {
        root ${ACME_WEBROOT};
    }

    location / {
        proxy_pass http://${UPSTREAM_HOST}:${UPSTREAM_PORT};
        proxy_http_version 1.1;

        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_connect_timeout 60s;
        proxy_send_timeout ${PROXY_READ_TIMEOUT};
        proxy_read_timeout ${PROXY_READ_TIMEOUT};
    }
}
NGINX

  install_nginx_config "$tmp_file"
}

write_https_config() {
  local tmp_file
  local server_name_line
  tmp_file="$(mktemp)"
  server_name_line="$(server_names)"

  log "Writing HTTPS nginx site config for ${server_name_line} -> ${UPSTREAM_HOST}:${UPSTREAM_PORT}..."
  cat >"$tmp_file" <<NGINX
server {
    listen 80;
    listen [::]:80;
    server_name ${server_name_line};

    location /.well-known/acme-challenge/ {
        root ${ACME_WEBROOT};
    }

    location / {
        return 301 https://\$host\$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name ${server_name_line};

    ssl_certificate /etc/letsencrypt/live/${DOMAIN}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${DOMAIN}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;

    client_max_body_size ${CLIENT_MAX_BODY_SIZE};

    location / {
        proxy_pass http://${UPSTREAM_HOST}:${UPSTREAM_PORT};
        proxy_http_version 1.1;

        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_connect_timeout 60s;
        proxy_send_timeout ${PROXY_READ_TIMEOUT};
        proxy_read_timeout ${PROXY_READ_TIMEOUT};
    }
}
NGINX

  install_nginx_config "$tmp_file"
}

install_nginx_config() {
  local tmp_file="$1"

  ${SUDO} mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
  ${SUDO} mv "$tmp_file" "/etc/nginx/sites-available/${SITE_NAME}"
  ${SUDO} ln -sfn "/etc/nginx/sites-available/${SITE_NAME}" "/etc/nginx/sites-enabled/${SITE_NAME}"

  if [ -e /etc/nginx/sites-enabled/default ]; then
    ${SUDO} rm -f /etc/nginx/sites-enabled/default
  fi
}

issue_certificate() {
  if [ "$ENABLE_SSL" != "true" ]; then
    log "ENABLE_SSL=false, skipping certificate issuance."
    return
  fi

  require_value "$LETSENCRYPT_EMAIL" "LETSENCRYPT_EMAIL"

  local staging_args=""
  if [ "$CERTBOT_STAGING" = "true" ]; then
    staging_args="--staging"
  fi

  log "Requesting Let's Encrypt certificate..."
  # shellcheck disable=SC2046
  ${SUDO} certbot certonly --webroot \
    -w "$ACME_WEBROOT" \
    $(certbot_domain_args) \
    --email "$LETSENCRYPT_EMAIL" \
    --agree-tos \
    --non-interactive \
    --keep-until-expiring \
    ${staging_args}

  if command -v systemctl >/dev/null 2>&1; then
    log "Enabling certbot auto-renew timer..."
    ${SUDO} systemctl enable --now certbot.timer
    ${SUDO} mkdir -p /etc/letsencrypt/renewal-hooks/deploy
    printf '%s\n' '#!/usr/bin/env sh' 'systemctl reload nginx >/dev/null 2>&1 || true' \
      | ${SUDO} tee /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh >/dev/null
    ${SUDO} chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh
  fi
}

reload_nginx() {
  log "Testing nginx config..."
  ${SUDO} nginx -t

  if command -v systemctl >/dev/null 2>&1; then
    ${SUDO} systemctl enable --now nginx
    ${SUDO} systemctl reload nginx
  else
    ${SUDO} service nginx reload
  fi
}

main() {
  require_value "$DOMAIN" "DOMAIN"
  install_packages

  if [ "$ENABLE_SSL" = "true" ]; then
    write_acme_http_config
    reload_nginx
    issue_certificate
    write_https_config
    reload_nginx
  else
    write_http_proxy_config
    reload_nginx
  fi

  log "Done. Site ${SITE_NAME} is configured for $(server_names)."
}

main "$@"
