# syntax=docker/dockerfile:1
ARG GO_VERSION=1.22

FROM golang:${GO_VERSION}-bullseye AS base
WORKDIR /src
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,source=go.sum,target=go.sum \
    --mount=type=bind,source=go.mod,target=go.mod \
    go mod download -x
RUN dpkg --add-architecture armel
RUN apt-get update && apt-get install -y libasound2-dev:armel gcc-arm-linux-gnueabi

FROM base as build-app
WORKDIR /src
ARG CGO_ENABLED=1
ARG CC=arm-linux-gnueabi-gcc
ARG PKG_CONFIG_PATH=/usr/lib/arm-linux-gnueabi/pkgconfig
ARG GOOS
ARG GOARCH
ARG GOARM
RUN --mount=type=bind,target=. find /src -type f
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,target=. \
    GOOS=${GOOS} GOARCH=${GOARCH} GOARM=${GOARM} go build -o /out/haptics-${GOOS}-${GOARCH}-${GOARM} ./cmd/haptics/main.go
RUN --mount=type=cache,target=/go/pkg/mod/ \
    --mount=type=bind,target=. \
    GOOS=${GOOS} GOARCH=${GOARCH} GOARM=${GOARM} go build -o /out/pirateaudio-${GOOS}-${GOARCH}-${GOARM} ./cmd/pirateaudio/main.go



FROM scratch AS binaries
ARG GOOS
ARG GOARCH
ARG GOARM
COPY --from=build-app /out/*-${GOOS}-${GOARCH}-${GOARM} /
