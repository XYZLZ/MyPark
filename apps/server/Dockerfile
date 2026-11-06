# syntax=docker/dockerfile:1
#
# Builds one MyPark server binary, selected by CMD (api|worker|simulator|
# migrate|seed), into a distroless image. The build context is apps/server.
#
#   docker build --build-arg CMD=api --build-arg VERSION=1.2.3 -t mypark-api apps/server
#
# The binary lives at /app/<CMD>. ENTRYPOINT cannot expand a build arg, so
# /app/entrypoint is a symlink to it. Health checks call the real path, e.g.
# ["CMD", "/app/api", "-healthcheck"].

FROM golang:1.27 AS build
WORKDIR /src

# Module download is its own layer: it is reused until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG CMD=api
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    test -d "./cmd/${CMD}" \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
       -o "/out/${CMD}" "./cmd/${CMD}" \
    && ln -s "/app/${CMD}" /out/entrypoint

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
USER nonroot
ENTRYPOINT ["/app/entrypoint"]
