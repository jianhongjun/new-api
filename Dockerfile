FROM oven/bun:1@sha256:0733e50325078969732ebe3b15ce4c4be5082f18c4ac1a0f0ca4839c2e4e42a7 AS builder

WORKDIR /build
COPY web/package.json .
COPY web/bun.lock .
RUN bun install
COPY ./web .
COPY ./VERSION .
RUN DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION=$(cat VERSION) bun run build

FROM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder2
ENV GO111MODULE=on CGO_ENABLED=0

ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

ADD go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=builder /build/dist ./web/dist
RUN go build -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=$(cat VERSION)'" -o new-api

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a

# 可选：访问 deb.debian.org 被代理/劫持导致 InRelease「invalid signature」时，构建时换镜像源，例如
#   docker compose build --build-arg DEBIAN_APT_DEB=http://mirrors.aliyun.com/debian --build-arg DEBIAN_APT_SEC=http://mirrors.aliyun.com/debian-security
# 须先替换 security 再替换 debian，避免子串误替换。
ARG DEBIAN_APT_DEB=http://deb.debian.org/debian
ARG DEBIAN_APT_SEC=http://deb.debian.org/debian-security
RUN set -eux; \
    for f in /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list; do \
      [ -f "$f" ] || continue; \
      sed -i "s|https://security.debian.org/debian-security|${DEBIAN_APT_SEC}|g" "$f"; \
      sed -i "s|http://security.debian.org/debian-security|${DEBIAN_APT_SEC}|g" "$f"; \
      sed -i "s|https://deb.debian.org/debian-security|${DEBIAN_APT_SEC}|g" "$f"; \
      sed -i "s|http://deb.debian.org/debian-security|${DEBIAN_APT_SEC}|g" "$f"; \
      sed -i "s|https://deb.debian.org/debian|${DEBIAN_APT_DEB}|g" "$f"; \
      sed -i "s|http://deb.debian.org/debian|${DEBIAN_APT_DEB}|g" "$f"; \
    done; \
    apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
