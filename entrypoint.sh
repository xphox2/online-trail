#!/bin/sh
set -e

DOMAIN="${ORS_TRAIL_DOMAIN:-}"
EMAIL="${ORS_TRAIL_EMAIL:-noreply@example.com}"

# If no domain or domain is localhost, run HTTP-only mode
if [ -z "$DOMAIN" ] || [ "$DOMAIN" = "localhost" ]; then
    echo "=== Online Trail - HTTP mode (port 8080) ==="
    exec ./online-trail -http 8080
fi

# SSL mode: generate Caddyfile and run both Caddy + game server
echo "=== Online Trail - SSL mode ==="
echo "  Domain: $DOMAIN"
echo "  Email:  $EMAIL"

cat > /etc/caddy/Caddyfile << EOF
{
    admin off
    email $EMAIL
    storage file_system /data
}

$DOMAIN {
    reverse_proxy localhost:8080 {
        header_up Host {host}
    }
}
EOF

# Start game server in background
./online-trail -http 8080 &
GO_PID=$!

# Graceful shutdown: kill Go app when Caddy exits or container stops
trap "kill $GO_PID 2>/dev/null; exit 0" INT TERM

# Start Caddy in foreground
caddy run --config /etc/caddy/Caddyfile &
CADDY_PID=$!

# Wait for either process to exit
wait $CADDY_PID $GO_PID
