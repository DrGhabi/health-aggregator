# Build stage
FROM golang:1.26-alpine AS builder

ENV GO_VERSION=1.26.5
ENV GOROOT /usr/local/go
ENV GOPATH /go
ENV PATH $GOROOT/bin:$GOPATH/bin:$PATH


RUN mkdir -p ${GOROOT} ${GOPATH}/src ${GOPATH}/bin /app

COPY --from=golang:1.26.5-alpine3.23 /usr/local/go/ /usr/local/go/

RUN XC_ARCH=amd64 && \
    XC_OS=linux && \
    export XC_ARCH && \
    export XC_OS && \
    set -xe && \
    apk upgrade --no-cache && \
    rm -rf /var/cache/apk/* && \
	go version

WORKDIR /app

# Copy go.mod and go.sum if available
COPY go.mod ./
# COPY go.sum ./
# RUN go mod download

# Copy the source code
COPY . .

RUN go mod vendor

# Build the application
# We use -o health-aggregator (no .exe for Linux container)
RUN go build -mod vendor -v -o health-aggregator ./cmd/health-aggregator

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
CMD ["/app/health-aggregator"]
