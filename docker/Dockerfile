FROM alpine:latest

RUN apk add --no-cache ca-certificates curl jq

ARG SURGE_VERSION=""

RUN ARCH=$(uname -m) && \
    case ${ARCH} in \
        x86_64) SURGE_ARCH="amd64" ;; \
        aarch64) SURGE_ARCH="arm64" ;; \
        armv7l) SURGE_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
    esac && \
    if [ -z "$SURGE_VERSION" ]; then \
        echo "Fetching latest surge release..."; \
        SURGE_VERSION=$(curl -s https://api.github.com/repos/surge-downloader/surge/releases/latest | jq -r .tag_name | sed 's/^v//'); \
    else \
        echo "Using specified surge version: $SURGE_VERSION"; \
    fi && \
    echo "Downloading surge v${SURGE_VERSION} for architecture: ${SURGE_ARCH}" && \
    curl -L -o /tmp/surge.tar.gz \
        "https://github.com/surge-downloader/surge/releases/download/v${SURGE_VERSION}/surge_${SURGE_VERSION}_linux_${SURGE_ARCH}.tar.gz" && \
    tar -xzf /tmp/surge.tar.gz -C /usr/local/bin && \
    rm /tmp/surge.tar.gz && \
    chmod +x /usr/local/bin/surge && \
    echo "Installed surge v${SURGE_VERSION}" && \
    apk del curl jq

RUN mkdir -p /root/.surge /downloads

WORKDIR /downloads

EXPOSE 1700

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD surge server status || exit 1
    
CMD ["surge", "server", "start"]