# Dockerfile for kandev-plugin-youtrack — builds all platform binaries and the
# installable plugin package without requiring a local Go toolchain.
#
# Build context must include both the plugin source and the kandev backend
# source (for the go.mod replace directive). Run from D:\KimoStore:
#
#   docker build -t kandev-plugin-youtrack:build -f kandev-plugin-youtrack/Dockerfile .
#
# Or use the helper script:
#   powershell -File kandev-plugin-youtrack/scripts/docker-build.ps1
#
# Extract the binaries from the image:
#   docker create --name yt-export kandev-plugin-youtrack:build
#   docker cp yt-export:/out ./server
#   docker rm yt-export
#
# Or use BuildKit --output (Docker Desktop 4.34+):
#   DOCKER_BUILDKIT=1 docker build --output ./out -f kandev-plugin-youtrack/Dockerfile .

FROM golang:1.26.1 AS builder

WORKDIR /workspace

COPY kandev-cloned/apps/backend /workspace/kandev-cloned/apps/backend
COPY kandev-plugin-youtrack /workspace/kandev-plugin-youtrack

WORKDIR /workspace/kandev-plugin-youtrack

RUN go mod tidy

RUN go vet ./...
RUN go test ./...

RUN mkdir -p /out/server

RUN CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/server/plugin-linux-amd64    ./cmd/kandev-plugin-youtrack
RUN CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /out/server/plugin-linux-arm64    ./cmd/kandev-plugin-youtrack
RUN CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/server/plugin-darwin-amd64   ./cmd/kandev-plugin-youtrack
RUN CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o /out/server/plugin-darwin-arm64  ./cmd/kandev-plugin-youtrack
RUN CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/server/plugin-windows-amd64.exe ./cmd/kandev-plugin-youtrack

COPY kandev-plugin-youtrack/manifest.yaml /out/manifest.yaml
COPY kandev-plugin-youtrack/ui /out/ui
COPY kandev-plugin-youtrack/README.md /out/README.md

RUN ls -la /out/server/

FROM scratch
COPY --from=builder /out /