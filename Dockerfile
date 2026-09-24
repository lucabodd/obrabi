# syntax=docker/dockerfile:1
#
# One image with the five Obrabi binaries; docker-compose.yml starts each
# service from it with its own command, so they deploy and scale separately.

FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for svc in gateway auth projects stats feedback; do \
      go build -ldflags="-s -w -X github.com/lucabodd/obrabi/internal/platform/buildinfo.Version=${VERSION}" \
        -o /out/$svc ./cmd/$svc || exit 1; \
    done

# Distroless: no shell, no package manager, runs as an unprivileged user.
# Time zone data is embedded in the binaries (time/tzdata).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
USER nonroot:nonroot
ENV OBRABI_LOG_FORMAT=json
EXPOSE 8080
CMD ["/app/gateway"]
