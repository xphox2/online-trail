# Online Trail - Multiplayer Go Implementation

A faithful multiplayer implementation of the classic 1840s westward journey game in Go.

## Quick Start

### Docker (Recommended)

**Local / HTTP only:**
```bash
docker run -d -p 8080:8080 --name online-trail xphox/online-trail
```
Then open http://localhost:8080

**Production with SSL (Let's Encrypt):**
```bash
docker run -d \
    --name online-trail \
    -p 80:80 -p 443:443 \
    -e ORS_TRAIL_DOMAIN=trail.example.com \
    -e ORS_TRAIL_EMAIL=you@example.com \
    -v online_trail_ssl:/data \
    --restart unless-stopped \
    xphox/online-trail
```

This will automatically:
- Obtain an SSL certificate from Let's Encrypt
- Serve the game over HTTPS
- Redirect HTTP to HTTPS
- Auto-renew certificates before expiration
- Persist certificates across container restarts

**Requirements for SSL:**
- Ports 80 and 443 must be open on your firewall
- Your domain's DNS must point to the server's public IP

### Docker Compose

```bash
# HTTP only (localhost)
docker compose up -d

# With SSL
ORS_TRAIL_DOMAIN=trail.example.com ORS_TRAIL_EMAIL=you@example.com docker compose up -d
```

### Build from Source

```bash
cd online-trail
go build -o server ./cmd/server
./server -http 8080
```
Then open http://localhost:8080

### Build Docker Image Locally

```bash
cd online-trail
docker build -t online-trail .
docker run -d -p 8080:8080 --name online-trail online-trail
```

## Game Features

- **Multiplayer**: Multiple players can join and play together
- **Two Game Modes**: Continuous (24/7, join anytime) or Scheduled (wait for players, start together)
- **Session Persistence**: Close your browser and resume where you left off
- **Real-time Updates**: See other players' actions live
- **Scoreboard**: Track all players' progress

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ORS_TRAIL_DOMAIN` | _(none)_ | Set to your domain to enable SSL. Leave unset or `localhost` for HTTP-only mode. |
| `ORS_TRAIL_EMAIL` | `noreply@example.com` | Email for Let's Encrypt certificate notifications. |
