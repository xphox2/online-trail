#!/bin/bash
# Online Trail - Single Command Setup
# Usage: 
#   HTTP only:   ./run.sh
#   HTTPS/SSL:   ORS_TRAIL_DOMAIN=game.example.com ORS_TRAIL_EMAIL=you@example.com ./run.sh

set -e

DOMAIN=${ORS_TRAIL_DOMAIN:-localhost}
EMAIL=${ORS_TRAIL_EMAIL:-noreply@example.com}

echo "Online Trail Setup"
echo "Domain: $DOMAIN"
echo "Email: $EMAIL"
echo ""

# Create .env for docker-compose
cat > .env << EOF
ORS_TRAIL_DOMAIN=$DOMAIN
ORS_TRAIL_EMAIL=$EMAIL
EOF

echo "Building and starting containers..."
docker-compose -f docker-compose.ssl.yml up --build -d

echo ""
echo "=========================================="
if [ "$DOMAIN" = "localhost" ]; then
    echo "Online Trail is running!"
    echo "Open: http://localhost"
else
    echo "Online Trail is running with HTTPS!"
    echo "Open: https://$DOMAIN"
    echo ""
    echo "SSL certificate will be ready in ~30-60 seconds"
fi
echo "=========================================="
echo ""
echo "Logs: docker-compose -f docker-compose.ssl.yml logs -f"
