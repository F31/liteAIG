# syntax=docker/dockerfile:1

# Lite AI gateway is a single self-contained binary: the Admin API, the data
# plane, and the embedded Console are all served by ./cmd/liteaig. The console
# dist is tracked in the repo, so no Node build happens here. The same image
# serves both the direct-process deployment model (docker run / host process)
# and the split-plane Helm chart.
#
# State is external: SQLite at /data (mount a volume) for the Lite tier, or
# Postgres + Redis via env for the Standard tier.

# --- build stage ---
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/liteaig ./cmd/liteaig

# --- run stage ---
# distroless nonroot = uid 65532, no shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/liteaig /liteaig
WORKDIR /data

VOLUME /data
# ready / admin / gateway (documentation only; the real ports are CLI flags).
EXPOSE 8080 8081 8082

ENTRYPOINT ["/liteaig"]
CMD ["--db", "file:/data/liteaig.db"]
