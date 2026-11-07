# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
# Fail the build instead of silently downloading a different toolchain.
ENV GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate


FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app -H -h /app app
WORKDIR /app
COPY --from=build /out/api /out/migrate ./
USER app
EXPOSE 8080
ENTRYPOINT ["./api"]
