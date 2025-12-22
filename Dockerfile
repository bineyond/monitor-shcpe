# Build stage
FROM golang:latest AS builder
WORKDIR /app

# Set proxy if needed (optional, good for specialized environments but clean for general use)
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o monitor main.go

# Final stage
FROM ubuntu:22.04

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install dependencies including Google Chrome for chromedp
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    gnupg \
    tzdata \
    fonts-liberation \
    libasound2 \
    libnspr4 \
    libnss3 \
    libu2f-udev \
    vulkan-tools \
    libvulkan1 \
    xdg-utils \
    && curl -fsSL https://dl.google.com/linux/linux_signing_key.pub | gpg --dearmor -o /usr/share/keyrings/google-chrome.gpg \
    && echo "deb [arch=amd64 signed-by=/usr/share/keyrings/google-chrome.gpg] http://dl.google.com/linux/chrome/deb/ stable main" > /etc/apt/sources.list.d/google-chrome.list \
    && apt-get update \
    && apt-get install -y google-chrome-stable \
    && rm -rf /var/lib/apt/lists/*

# Set timezone to Asia/Shanghai by default (can be overridden)
ENV TZ=Asia/Shanghai

WORKDIR /app

# Copy binary and assets
COPY --from=builder /app/monitor .
COPY --from=builder /app/static ./static

# Create a volume for data persistence (optional but recommended)
# Users can mount a host directory to /app/data and symlink, or just mount /app/monitor.db
# We'll just document how to use it.

# Expose port
EXPOSE 8080

# Run the application
CMD ["./monitor"]
