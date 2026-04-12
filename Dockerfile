# Stage 1: Build the Go binaries
FROM golang:1.25-alpine AS builder

# Set working directory
WORKDIR /build

# Copy all source code
COPY . .

# Build binaries with automatic architecture detection
ARG TARGETARCH
ENV GOARCH=${TARGETARCH:-amd64}
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o localsend-hub . && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o localsend-hub-admin ./cmd/admin

# Stage 2: Create minimal runtime image
FROM alpine:3.19

# Add labels
LABEL maintainer="LocalSend Hub"
LABEL description="LocalSend Hub - LocalSend Receiver"
LABEL version="2.0.0"

# Install CA certificates (for HTTPS) and dumb-init for proper process management
RUN apk --no-cache add ca-certificates tzdata dumb-init

# Set working directory
WORKDIR /app

# Copy binaries from builder
COPY --from=builder /build/localsend-hub /app/localsend-hub
COPY --from=builder /build/localsend-hub-admin /app/localsend-hub-admin

# Copy entrypoint script
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Create directory for received files
RUN mkdir -p /app/received

# Create directory for config
RUN mkdir -p /app/config

# Create directory for SQLite data
RUN mkdir -p /app/data

# Set permissions
RUN chmod +x /app/localsend-hub /app/localsend-hub-admin

# Expose ports
# 53317: LocalSend HTTPS (Core API)
# 53318: Admin Console (HTTP)
EXPOSE 53317/tcp 53318/tcp

# Volume for persistent data
VOLUME ["/app/received", "/app/config", "/app/data"]

# Health check (core service)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider https://localhost:53317/api/localsend/v2/info --no-check-certificate || exit 1

# Use entrypoint script
ENTRYPOINT ["/usr/bin/dumb-init", "--", "/app/entrypoint.sh"]
