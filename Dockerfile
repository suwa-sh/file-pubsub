# file-pubsub container image: a single static binary on a distroless base.
# The configuration is mounted at runtime (default: /etc/file-pubsub/config.yaml).
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/file-pubsub ./cmd/file-pubsub

FROM gcr.io/distroless/static-debian12:latest
COPY --from=build /out/file-pubsub /usr/local/bin/file-pubsub
ENTRYPOINT ["/usr/local/bin/file-pubsub"]
CMD ["serve", "--config", "/etc/file-pubsub/config.yaml"]
