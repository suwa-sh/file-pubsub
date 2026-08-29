# file-pubsub container image: a single static binary on a distroless base.
# The configuration is mounted at runtime (default: /etc/file-pubsub/config.yaml).
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/file-pubsub ./cmd/file-pubsub

FROM gcr.io/distroless/static-debian12:latest@sha256:22fd79fd75eab2372585b44517f8a094349938919dc613aafc37e4bdc9967c82
COPY --from=build /out/file-pubsub /usr/local/bin/file-pubsub
ENTRYPOINT ["/usr/local/bin/file-pubsub"]
CMD ["serve", "--config", "/etc/file-pubsub/config.yaml"]
