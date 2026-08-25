FROM golang:1.26-bookworm AS build
WORKDIR /src

COPY DotaPublic/go.mod ./
COPY DotaPublic/cmd ./cmd
COPY DotaPublic/internal ./internal
RUN go test ./... \
    && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/dota-public-server ./cmd/server \
    && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/dota-public-exporter ./cmd/exporter \
    && CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/dota-public-publisher ./cmd/publisher

FROM debian:bookworm-slim AS runtime
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 1000 --create-home dota-public
WORKDIR /app
USER dota-public

FROM runtime AS server
COPY --from=build /out/dota-public-server /app/dota-public-server
COPY --chown=1000:1000 DotaLocalHub/web /app/web
COPY --chown=1000:1000 DotaPublic/web /app/web
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD curl --fail --silent http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/dota-public-server"]

FROM runtime AS exporter
COPY --from=build /out/dota-public-exporter /app/dota-public-exporter
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["/app/dota-public-exporter", "healthcheck"]
ENTRYPOINT ["/app/dota-public-exporter"]

FROM debian:bookworm-slim AS publisher
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 1000 --create-home dota-public
WORKDIR /app
COPY --from=build /out/dota-public-publisher /app/dota-public-publisher
COPY --chown=1000:1000 DotaLocalHub/web /app/web-base
COPY --chown=1000:1000 DotaPublic/web /app/web-public
COPY --chown=1000:1000 DotaPublic/github-known-hosts /app/github-known-hosts
USER dota-public
HEALTHCHECK --interval=1m --timeout=10s --start-period=2m --retries=3 \
  CMD ["/app/dota-public-publisher", "healthcheck"]
ENTRYPOINT ["/app/dota-public-publisher"]
