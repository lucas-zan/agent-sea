# Agent Engine Development Environment
# Multi-language: Go + Python + Node.js

FROM golang:1.24-bookworm

# Install Python and Node.js
RUN apt-get update && apt-get install -y \
    python3 \
    python3-pip \
    python3-venv \
    nodejs \
    npm \
    git \
    curl \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Create symlink for python command
RUN ln -sf /usr/bin/python3 /usr/bin/python

# Set working directory
WORKDIR /app

# Keep container running for development
CMD ["tail", "-f", "/dev/null"]
