# Use an official Go runtime as a parent image
# Specify the Go version you are using
FROM golang:1.24 AS builder

# Set the working directory inside the container
WORKDIR /app

# Install libibverbs development package
# Required for cgo to link against libibverbs
RUN apt-get update && apt-get install -y --no-install-recommends \
    libibverbs-dev \
    gcc \
    libc-dev \
    && rm -rf /var/lib/apt/lists/*

# Copy the Go module files
COPY go.mod ./

# Copy the source code
COPY main.go rdma.go exchange.go ./

# Build the Go application
# CGO_ENABLED=1 is necessary to build code that uses cgo
# -ldflags "-s -w" can make the binary smaller, optional
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -v -o /go-rocev2-ud-pingpong .

# --- Final Stage --- Use a smaller base image for the final artifact
# Using debian:stable-slim as an example. Adjust if needed.
FROM debian:stable-slim

WORKDIR /root/

# Install runtime dependency: libibverbs1
RUN apt-get update && apt-get install -y --no-install-recommends \
    libibverbs1 \
    && rm -rf /var/lib/apt/lists/*

# Copy the pre-built binary from the builder stage
COPY --from=builder /go-rocev2-ud-pingpong .

# Command to run the executable
# The entrypoint will be specified in the Makefile or docker run command
# ENTRYPOINT ["/root/go-rocev2-ud-pingpong"]
# CMD ["--help"] # Default command
