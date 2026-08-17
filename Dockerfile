# syntax=docker/dockerfile:1

# Build stage: Go 1.26 toolchain (pinned, never "latest"). The builder runs on
# the build host platform and cross-compiles to the requested target so the same
# Dockerfile supports linux/amd64 and linux/arm64.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/arcticexpress ./cmd/server

# Runtime stage: scratch image keeps only the compiled binary.
FROM scratch
COPY --from=builder /out/arcticexpress /arcticexpress
EXPOSE 58021
ENTRYPOINT ["/arcticexpress"]
