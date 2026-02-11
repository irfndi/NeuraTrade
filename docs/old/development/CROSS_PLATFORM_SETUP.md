# Cross-Platform Docker Setup Guide

## Overview
This guide addresses cross-platform compatibility issues when running the NeuraTrade Go application across macOS, Windows, and Linux environments.

## Host Resolution Issues

### Problem
`host.docker.internal` resolves automatically on Docker Desktop (macOS/Windows), but requires additional configuration on native Linux Docker installations.

### Solutions for Linux

#### Option 1: Use Docker Host Gateway (Recommended)
Add the following to your `docker-compose.yml` for Linux compatibility:

```yaml
services:
  nginx:
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

#### Option 2: Use Linux-Specific Config Files
Create platform-specific nginx configurations:

**For Linux (nginx.linux.conf):**
```nginx
upstream app {
    server 172.17.0.1:8080;  # Default Docker bridge gateway
}

upstream ccxt {
    server 172.17.0.1:3001;
}
```

**For Linux (nginx.linux.test.conf):**
```nginx
upstream app {
    server 172.17.0.1:8080;
}

upstream ccxt {
    server 172.17.0.1:3001;
}
```

#### Option 3: Environment Variable Substitution
Use environment variables in nginx configs with variable substitution:

```nginx
upstream app {
    server ${DOCKER_HOST_IP:-host.docker.internal}:8080;
}
```

Then set `DOCKER_HOST_IP=172.17.0.1` in your `.env` file on Linux.

## Quick Setup Commands

### Linux Setup Script
```bash
#!/bin/bash
# run-linux.sh

# Detect if running on Linux
if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    echo "Linux detected - applying host resolution fix..."
    
    # Option 1: Use host-gateway
    export DOCKER_HOST_IP="host-gateway"
    
    # Or Option 2: Use bridge IP
    # export DOCKER_HOST_IP="$(docker network inspect bridge | jq -r '.[0].IPAM.Config[0].Gateway')"
    
    echo "Using Docker host: $DOCKER_HOST_IP"
fi

# Continue with standard startup
docker-compose up
```

## Environment Detection

### Makefile Integration
Add to your Makefile for automatic detection:

```makefile
# Detect OS and set appropriate host
ifeq ($(shell uname -s),Linux)
    DOCKER_HOST_IP := $(shell docker network inspect bridge | grep -oP '(?<="Gateway": ")[^"]*')
else
    DOCKER_HOST_IP := host.docker.internal
endif

linux-setup:
	@echo "Setting up for Linux..."
	@echo "DOCKER_HOST_IP=$(DOCKER_HOST_IP)" >> .env
	@echo "Linux setup complete. Run 'make docker-run' to start services."
```

## Verification

### Test Host Resolution
```bash
# Test if host.docker.internal resolves
docker run --rm alpine nslookup host.docker.internal

# On Linux, test bridge gateway
docker run --rm alpine ping 172.17.0.1
```

## Troubleshooting

### Common Issues on Linux
1. **Connection refused**: Check if services are binding to 0.0.0.0 instead of localhost
2. **Network unreachable**: Verify Docker bridge network configuration
3. **Permission denied**: Ensure Docker daemon is running and user has permissions

### Debug Commands
```bash
# Check Docker networks
docker network ls
docker network inspect bridge

# Test connectivity
docker run --rm alpine wget -qO- http://172.17.0.1:8080/health
```

## Platform-Specific Docker Compose

### Linux-Specific Override
Create `docker-compose.linux.yml`:

```yaml
version: '3.8'
services:
  nginx:
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

Usage:
```bash
docker-compose -f docker-compose.yml -f docker-compose.linux.yml up
```

## Summary

| Platform | Host Resolution Method |
|----------|----------------------|
| macOS    | host.docker.internal (automatic) |
| Windows  | host.docker.internal (automatic) |
| Linux    | 172.17.0.1 or host-gateway |

Choose the method that best fits your Linux distribution and Docker setup.