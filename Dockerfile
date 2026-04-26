FROM --platform=$BUILDPLATFORM node:24-alpine AS web-builder
WORKDIR /workspace/web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder
WORKDIR /workspace/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown
RUN target_os="${TARGETOS:-$(go env GOOS)}" && \
    target_arch="${TARGETARCH:-$(go env GOARCH)}" && \
    echo "Building backend for ${target_os}/${target_arch}" && \
    CGO_ENABLED=0 GOOS="${target_os}" GOARCH="${target_arch}" \
      go build \
      -ldflags="-s -w -X chatgpt2api/internal/buildinfo.Version=${VERSION} -X chatgpt2api/internal/buildinfo.Commit=${COMMIT} -X chatgpt2api/internal/buildinfo.BuildTime=${BUILD_TIME}" \
      -o /out/chatgpt2api-studio .

FROM --platform=$BUILDPLATFORM alpine:3.22 AS runtime-assets
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

FROM alpine:3.22

WORKDIR /app

COPY --from=runtime-assets /etc/ssl/certs /etc/ssl/certs
COPY --from=runtime-assets /usr/share/zoneinfo /usr/share/zoneinfo

COPY --from=backend-builder /out/chatgpt2api-studio /app/chatgpt2api-studio
COPY backend/internal/config/config.defaults.toml /app/data/config.example.toml
COPY --from=web-builder /workspace/web/dist /app/static

EXPOSE 7000

CMD ["./chatgpt2api-studio"]
