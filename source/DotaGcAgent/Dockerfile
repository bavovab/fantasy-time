FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# The pinned go-steam release obtains a persistent refresh token but does not
# expose it to callers. Keep this tiny audited patch until upstream exposes it.
RUN STEAM_AUTH="$(go env GOPATH)/pkg/mod/github.com/paralin/go-steam@v0.0.0-20260627095526-9a903700fc88/auth.go" \
    && chmod u+w "$STEAM_AUTH" \
    && sed -i '/^[[:space:]]*language := "english"$/i\    details.AccessToken = accessToken' "$STEAM_AUTH" \
    && grep -q 'details.AccessToken = accessToken' "$STEAM_AUTH"
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w -X main.version=1.1.1 -X main.buildDate=2026-07-22" \
    -o /out/dota-gc-agent .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates wget \
    && addgroup -S dota-gc \
    && adduser -S -G dota-gc -h /nonexistent dota-gc

COPY --from=build /out/dota-gc-agent /usr/local/bin/dota-gc-agent

USER dota-gc
EXPOSE 8788
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8788/api/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/dota-gc-agent"]
