#!/bin/bash
# server-setup.sh
#
# One-time setup for ecomist.mchugh.au on Linode Debian.
# Run as root or with sudo:
#   curl -fsSL https://raw.githubusercontent.com/exploded/ecomist/master/scripts/server-setup.sh | sudo bash
#
# Assumes Caddy and the "deploy" user (with the GitHub Actions SSH key) already
# exist from an earlier project's setup (e.g. moon/train). Caddy auto-provisions
# TLS via Let's Encrypt.
#
# ecomist embeds its templates/static/migrations/tzdata, so no web assets are
# copied at deploy time - only the binary.
#
# After running, you still need to (manually):
#   1. Add DNS A + AAAA records ecomist.mchugh.au -> this server (DNS-only)
#   2. Add a site block to /etc/caddy/Caddyfile proxying to 127.0.0.1:8995
#      and run: sudo systemctl reload caddy
#   3. Edit /var/www/ecomist/.env with real Google OAuth + Anthropic values
#   4. systemctl enable --now ecomist

set -e

echo "=== ecomist - Server Deployment Setup ==="

# ---------------------------------------------------------------
# 1. Application directory
# ---------------------------------------------------------------
APP_DIR="/var/www/ecomist"
if [ -d "$APP_DIR" ]; then
    echo "[ok] Application directory $APP_DIR already exists"
else
    mkdir -p "$APP_DIR"
    chown www-data:www-data "$APP_DIR"
    echo "[ok] Created application directory $APP_DIR"
fi

# ---------------------------------------------------------------
# 2. .env template (edit with real credentials afterwards)
# ---------------------------------------------------------------
ENV_FILE="$APP_DIR/.env"
if [ -f "$ENV_FILE" ]; then
    echo "[ok] .env file already exists at $ENV_FILE (not overwriting)"
else
    cat > "$ENV_FILE" << 'ENV_TEMPLATE'
# --- Server ---
PORT=8995
DB_PATH=ecomist.db
BASE_URL=https://ecomist.mchugh.au
TZ=Australia/Melbourne

# --- Google OAuth (create a client at console.cloud.google.com; redirect URI
#     must be https://ecomist.mchugh.au/auth/google/callback) ---
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=

# --- The cross-franchise administrator ---
ADMIN_EMAIL=james67@gmail.com

# --- PDF run-sheet import (optional; feature hides itself when blank) ---
ANTHROPIC_API_KEY=
ENV_TEMPLATE
    chown www-data:www-data "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    echo "[ok] Created .env at $ENV_FILE - edit credentials before starting"
fi

# ---------------------------------------------------------------
# 3. systemd service
# ---------------------------------------------------------------
SERVICE_FILE="/etc/systemd/system/ecomist.service"
cat > "$SERVICE_FILE" << 'SERVICE'
[Unit]
Description=ecomist run sheets
After=network.target

[Service]
Type=simple
User=www-data
Group=www-data
WorkingDirectory=/var/www/ecomist
EnvironmentFile=/var/www/ecomist/.env
ExecStart=/var/www/ecomist/ecomist
Restart=on-failure
RestartSec=5

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
# SQLite needs to write its DB + WAL files in the working directory
ReadWritePaths=/var/www/ecomist

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
echo "[ok] Created systemd service at $SERVICE_FILE"

# ---------------------------------------------------------------
# 4. Install the deploy script (runs as root via sudo).
# ---------------------------------------------------------------
DEPLOY_SCRIPT_URL="https://raw.githubusercontent.com/exploded/ecomist/master/scripts/deploy-ecomist"
if ! curl -fsSL "$DEPLOY_SCRIPT_URL" -o /usr/local/bin/deploy-ecomist; then
    echo "[error] Failed to download deploy-ecomist from $DEPLOY_SCRIPT_URL"
    echo "        (If this is the first deploy and the repo is empty,"
    echo "         the deploy bundle's self-update logic will install it"
    echo "         on first deployment. You can also copy it manually.)"
fi
[ -f /usr/local/bin/deploy-ecomist ] && chmod +x /usr/local/bin/deploy-ecomist

# ---------------------------------------------------------------
# 5. sudoers - allow the existing deploy user to run our deploy script
# ---------------------------------------------------------------
SUDOERS_FILE="/etc/sudoers.d/ecomist-deploy"
cat > "$SUDOERS_FILE" << 'EOF'
deploy ALL=(ALL) NOPASSWD: /usr/local/bin/deploy-ecomist
deploy ALL=(ALL) NOPASSWD: /usr/bin/systemctl stop ecomist
EOF
chmod 440 "$SUDOERS_FILE"
visudo -c -f "$SUDOERS_FILE"
echo "[ok] sudoers entry created at $SUDOERS_FILE"

# ---------------------------------------------------------------
# 6. Caddy site block - print snippet for the user to add manually
# ---------------------------------------------------------------
echo "[note] Add the following block to /etc/caddy/Caddyfile, then run:"
echo "       sudo systemctl reload caddy"
echo "       Caddy will provision the TLS cert on first request."
cat << 'CADDY'

ecomist.mchugh.au {
    reverse_proxy 127.0.0.1:8995
}

CADDY

echo ""
echo "=== Setup complete ==="
echo "Next manual steps:"
echo "  1. DNS: point ecomist.mchugh.au A + AAAA records at this server (DNS-only)"
echo "  2. Add the Caddy block above to /etc/caddy/Caddyfile, then:"
echo "     sudo systemctl reload caddy"
echo "  3. Edit $ENV_FILE with the Google OAuth client + ANTHROPIC_API_KEY"
echo "  4. systemctl enable --now ecomist"
echo ""
echo "GitHub Actions secrets needed on exploded/ecomist:"
echo "  DEPLOY_HOST, DEPLOY_USER, DEPLOY_PORT, DEPLOY_SSH_KEY"
