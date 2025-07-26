FROM golang:1.24-alpine AS builder

ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache \
    gcc \
    make \
    musl-dev \
    opus-dev

WORKDIR /workspace

COPY go.mod go.sum ./

# Cache deps before building and copying source so that we don't need to re-download as much and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY *.go .

# Build the binary with CGO enabled so gopus works
ENV CGO_ENABLED=1

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o erika .

FROM alpine:latest

RUN apk add --no-cache ffmpeg

WORKDIR /
COPY --from=builder /workspace/erika .

CMD ["/erika"]