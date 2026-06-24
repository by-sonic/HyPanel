FROM --platform=$BUILDPLATFORM node:alpine AS front-builder
WORKDIR /app
COPY frontend/ ./
RUN npm install && npm run build

FROM golang:1.26-alpine AS backend-builder
WORKDIR /app
ARG TARGETARCH
ARG TARGETVARIANT
ENV CGO_ENABLED=1
ENV CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
ENV GOARCH=$TARGETARCH

RUN apk update && apk add --no-cache \
    gcc \
    musl-dev \
    libc-dev \
    make \
    git \
    wget \
    unzip \
    bash \
    curl

ENV CC=gcc

RUN CRONET_ARCH="$TARGETARCH" && \
    CRONET_URL="https://github.com/SagerNet/cronet-go/releases/latest/download/libcronet-linux-${CRONET_ARCH}.so"; \
    echo "Downloading $CRONET_URL" && \
    wget -q -O ./libcronet.so "$CRONET_URL" && \
    chmod 755 ./libcronet.so

# HyPanel: reconstruct the patched sing-box/sing-quic forks and warm the module
# cache in a layer keyed only on go.mod/go.sum + forks/ (the patches), so editing
# regular source below does NOT re-clone the upstreams or re-download modules.
# Ф1 (restart-free Hysteria2 user add/ban) calls methods upstream doesn't expose;
# without this overlay the build fails. See forks/README.md. setup.sh is
# idempotent and rewrites the go.mod replace directives to ./forks/*.
COPY go.mod go.sum ./
COPY forks/ ./forks/
RUN bash forks/setup.sh && go mod download

COPY . .
COPY --from=front-builder /app/dist/ /app/web/html/

# Re-apply the overlay + replace directives: COPY . . above restored the upstream
# go.mod and the host's (clone-less) forks/ tree. setup.sh is idempotent — the
# clones from the cached layer survive (COPY merges, never deletes), so this only
# re-checkouts, re-overlays the patched files, and repoints go.mod; then tidy
# reconciles go.sum against the now-present full source.
RUN bash forks/setup.sh && go mod tidy

RUN if [ "$TARGETARCH" = "arm" ]; then export GOARM=7; [ "$TARGETVARIANT" = "v6" ] && export GOARM=6; fi; \
    go build -ldflags="-w -s" \
    -tags "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_naive_outbound,with_purego,with_tailscale" \
    -o sui main.go

FROM alpine
LABEL org.opencontainers.image.authors="alireza7@gmail.com"
ENV TZ=Asia/Tehran
WORKDIR /app
RUN set -ex && apk add --no-cache --upgrade bash tzdata ca-certificates nftables
COPY --from=backend-builder /app/sui /app/libcronet.so /app/
COPY entrypoint.sh /app/
ENTRYPOINT [ "./entrypoint.sh" ]