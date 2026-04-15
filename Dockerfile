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

# 在 slim 镜像尚未 apt 安装 ca-certificates 前，用 Alpine 的 CA 包让 apt 能走 HTTPS（规避 HTTP InRelease 被代理篡改导致的 GPG 报错）
FROM alpine:3.20 AS ssl-bootstrap
RUN apk add --no-cache ca-certificates

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a
COPY --from=ssl-bootstrap /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# 可选：换 Debian 源（须与 bookworm 目录结构一致），例：
#   --build-arg DEBIAN_APT_DEB=http://mirrors.aliyun.com/debian --build-arg DEBIAN_APT_SEC=http://mirrors.aliyun.com/debian-security
# 下面会删掉镜像自带 deb822，避免 sed 残留重复条目；再把常见主机 http→https（dash/sh 下用 echo|sed 处理变量）。
ARG DEBIAN_APT_DEB=http://deb.debian.org/debian
ARG DEBIAN_APT_SEC=http://deb.debian.org/debian-security
RUN set -eux; \
    DEB_URI="${DEBIAN_APT_DEB}"; \
    SEC_URI="${DEBIAN_APT_SEC}"; \
    for h in \
      deb.debian.org \
      security.debian.org \
      mirrors.aliyun.com \
      mirrors.tuna.tsinghua.edu.cn \
      mirrors.huaweicloud.com \
      repo.huaweicloud.com \
      ; do \
      DEB_URI=$(echo "$DEB_URI" | sed "s|http://${h}|https://${h}|g"); \
      SEC_URI=$(echo "$SEC_URI" | sed "s|http://${h}|https://${h}|g"); \
    done; \
    rm -f /etc/apt/sources.list; \
    rm -f /etc/apt/sources.list.d/debian.sources; \
    printf '%s\n' \
      'Types: deb' \
      "URIs: ${DEB_URI}" \
      'Suites: bookworm bookworm-updates' \
      'Components: main' \
      'Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg' \
      '' \
      'Types: deb' \
      "URIs: ${SEC_URI}" \
      'Suites: bookworm-security' \
      'Components: main' \
      'Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg' \
      > /etc/apt/sources.list.d/debian.sources; \
    printf '%s\n' \
      'Acquire::http::Pipeline-Depth "0";' \
      'Acquire::https::Pipeline-Depth "0";' \
      'Acquire::Check-Valid-Until "false";' \
      'Acquire::Retries "5";' \
      'Acquire::http::Timeout "120";' \
      'Acquire::https::Timeout "120";' \
      > /etc/apt/apt.conf.d/99nopipeline; \
    i=0; \
    until apt-get update; do \
      i=$((i+1)); \
      if [ "$i" -ge 4 ]; then exit 1; fi; \
      sleep 5; \
    done; \
    apt-get install -y --no-install-recommends ca-certificates tzdata wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
