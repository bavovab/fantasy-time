FROM golang:1.26-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/dota-local-hub .

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 1000 --create-home dota \
    && mkdir -p /app/data/replays /app/data/json /app/data/tournaments /app/data/uploads /app/data/saved-replays /app/data/secrets \
    && chown -R dota:dota /app/data

COPY --from=build /out/dota-local-hub /app/dota-local-hub
COPY web /app/web

ENV DOTA_HUB_DIR=/app
ENV DOTA_HUB_LISTEN=0.0.0.0:8787

USER dota
EXPOSE 8787
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD curl --fail --silent http://127.0.0.1:8787/api/health >/dev/null || exit 1

ENTRYPOINT ["/app/dota-local-hub"]
