# Stage 1: Build the Go binary
FROM golang:1.21-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
COPY cmd ./cmd
COPY pkg ./pkg
COPY static ./static

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o online-trail ./cmd/server

# Stage 2: Get the Caddy binary
FROM caddy:2.8.4-alpine AS caddy

# Stage 3: Final image with both Go app and Caddy
FROM alpine:3.19

RUN apk add --no-cache ca-certificates && mkdir -p /etc/caddy /data /app/data

WORKDIR /app

# Copy Go binary and static files
COPY --from=builder /build/online-trail .
COPY --from=builder /build/static ./static
RUN chmod +x ./online-trail

# Copy Caddy binary
COPY --from=caddy /usr/bin/caddy /usr/bin/caddy

# Copy entrypoint
COPY entrypoint.sh .
RUN chmod +x ./entrypoint.sh

EXPOSE 80 443 8080

ENTRYPOINT ["./entrypoint.sh"]
