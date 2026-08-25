FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dota-live-coordinator .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && addgroup -S dota-live && adduser -S -G dota-live -h /nonexistent dota-live
COPY --from=build /out/dota-live-coordinator /usr/local/bin/dota-live-coordinator
USER dota-live
EXPOSE 8791
ENTRYPOINT ["/usr/local/bin/dota-live-coordinator"]
