FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN GOPROXY=$GOPROXY go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/youth-rehab ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/youth-rehab /app/youth-rehab
RUN useradd --system --uid 10001 --create-home rehab && mkdir -p /app/data && chown -R rehab:rehab /app
USER rehab
ENV HTTP_ADDR=:8080 DB_PATH=/app/data/rehab.db
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=2s --start-period=5s --retries=10 CMD curl --fail http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/youth-rehab"]
