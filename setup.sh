#!/bin/bash

# Online Trail - Single Container Setup with Optional SSL
# Can be run with or without domain for SSL

echo "======================================"
echo "  Online Trail - Game Server"
echo "======================================"
echo ""

# Default values from environment or parameters
DOMAIN=${1:-${ORS_TRAIL_DOMAIN:-""}}
EMAIL=${2:-${ORS_TRAIL_EMAIL:-""}}

# Show current configuration
if [ -n "$DOMAIN" ] && [ "$DOMAIN" != "localhost" ]; then
    echo "Mode: HTTPS with Let's Encrypt"
    echo "Domain: $DOMAIN"
    echo "Email: $EMAIL"
    MODE="ssl"
else
    echo "Mode: HTTP only (local)"
    MODE="http"
    DOMAIN="localhost"
    EMAIL="noreply@example.com"
fi

echo ""

# Stop any existing containers
echo "Stopping existing containers..."
docker-compose down 2>/dev/null || true

# Pull latest image
echo "Pulling latest image..."
docker-compose pull

# Start the container
echo ""
echo "Starting Online Trail..."

if [ "$MODE" = "ssl" ]; then
    # HTTPS mode with SSL
    docker run -d \
        --name online-trail \
        --restart unless-stopped \
        -p 80:80 -p 443:443 \
        -e ORS_TRAIL_DOMAIN="$DOMAIN" \
        -e ORS_TRAIL_EMAIL="$EMAIL" \
        -e ORS_MODE=ssl \
        -v online_trail_ssl:/data \
        xphox/online-trail
    
    echo ""
    echo "======================================"
    echo "Online Trail is running with HTTPS!"
    echo "======================================"
    echo ""
    echo "URLs:"
    echo "  HTTP:  http://$DOMAIN"
    echo "  HTTPS: https://$DOMAIN"
    echo ""
    echo "SSL certificates are stored in 'online_trail_ssl' volume"
    echo "They persist across container updates!"
else
    # HTTP only mode
    docker run -d \
        --name online-trail \
        --restart unless-stopped \
        -p 8080:8080 \
        -e ORS_TRAIL_DOMAIN=localhost \
        -e ORS_MODE=http \
        xphox/online-trail
    
    echo ""
    echo "======================================"
    echo "Online Trail is running!"
    echo "======================================"
    echo ""
    echo "URL: http://localhost:8080"
    echo ""
    echo "For HTTPS, run with:"
    echo "  ./setup.sh your-domain.com your@email.com"
fi

echo ""
echo "Commands:"
echo "  View logs: docker logs -f online-trail"
echo "  Stop:      docker stop online-trail"
echo "  Update:    docker pull xphox/online-trail && docker restart online-trail"
echo "             (SSL certificates are preserved!)"
