# Variables
IMAGE_NAME := go-rocev2-ud-pingpong
TAG := latest
DOCKERFILE := Dockerfile
BINARY_NAME := go-rocev2-ud-pingpong

# Default target
all: build

# Build the Docker image
build:
	@echo "Building Docker image $(IMAGE_NAME):$(TAG)..."
	docker build -t $(IMAGE_NAME):$(TAG) -f $(DOCKERFILE) .

# Extract the binary from the container to the local bin directory
extract-binary: build
	@echo "Extracting binary from container to local bin directory..."
	@mkdir -p bin
	@docker create --name temp_container $(IMAGE_NAME):$(TAG)
	@docker cp temp_container:/root/$(BINARY_NAME) bin/
	@docker rm temp_container
	@echo "Binary extracted to bin/$(BINARY_NAME)"

# Run the server in a container
# Requires host networking and privileged mode for RDMA access
# Adjust port and GID index as needed
run-server:
	@echo "Running server in container..."
	docker run --rm -it --net=host --privileged \
		$(IMAGE_NAME):$(TAG) /root/go-rocev2-ud-pingpong -i 1 -g 0

# Run the client in a container
# Requires host networking and privileged mode for RDMA access
# Replace <server_ip> with the actual server IP
# Adjust port and GID index as needed
run-client:
	@echo "Running client in container..."
	@read -p "Enter server IP address: " server_ip; \
	docker run --rm -it --net=host --privileged \
		$(IMAGE_NAME):$(TAG) /root/go-rocev2-ud-pingpong -i 1 -g 0 $$server_ip

# Remove the built Docker image
clean:
	@echo "Removing Docker image $(IMAGE_NAME):$(TAG)..."
	docker rmi $(IMAGE_NAME):$(TAG) || true
	@echo "Removing extracted binary..."
	@rm -f bin/$(BINARY_NAME)

# Remove dangling images (optional)
prune:
	@echo "Removing dangling Docker images..."
	docker image prune -f

.PHONY: all build extract-binary run-server run-client clean prune
