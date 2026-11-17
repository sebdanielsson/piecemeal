# syntax=docker/dockerfile:1

# Both build stages run on the build machine's platform: the web UI is
# platform-independent, and Go cross-compiles. Only the final stage is
# per-platform, and it has nothing to run, so multi-arch builds need no QEMU.

FROM --platform=$BUILDPLATFORM node:26.13.1-alpine AS web
# renovate: datasource=npm depName=pnpm
ARG PNPM_VERSION=12.20.0
RUN npm install --global "pnpm@${PNPM_VERSION}"
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
COPY web/tools/openapi-gen/package.json ./tools/openapi-gen/
RUN pnpm install --frozen-lockfile
COPY web/ ./
# Writes /src/internal/webui/dist (see web/vite.config.ts).
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine AS build
ARG TARGETOS TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/piecemeal ./cmd/piecemeal \
    && mkdir -p /out/data /out/downloads

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/piecemeal /piecemeal
# Writable by the nonroot user (65532) when no volume is mounted over them.
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/downloads /downloads
ENV PIECEMEAL_DATA_DIR=/data \
    PIECEMEAL_DOWNLOAD_DIR=/downloads
VOLUME ["/data"]
# 8080 serves the APIs and web UI; 6881 is the default peer port.
EXPOSE 8080 6881/tcp 6881/udp
USER nonroot:nonroot
ENTRYPOINT ["/piecemeal"]
