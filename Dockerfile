# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum if available
COPY go.mod ./
# COPY go.sum ./
# RUN go mod download

# Copy the source code
COPY . .

# Build the application
# We use -o health-aggregator (no .exe for Linux container)
RUN CGO_ENABLED=0 GOOS=linux go build -v -o health-aggregator ./cmd/health-aggregator

# Final stage
FROM alpine:latest

# Create a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

# Copy the binary from the builder stage
# Set ownership to the non-root user
COPY --from=builder --chown=appuser:appgroup /app/health-aggregator .

# Switch to the non-root user
USER appuser

# Run the application
CMD ["./health-aggregator"]
