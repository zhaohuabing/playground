# syntax=docker/dockerfile:1
#
# Builds libcomposer.so from this self-contained Go module and emits an OCI image
# whose root filesystem contains just the shared library, so it can be mounted as
# an Envoy Gateway image-volume at /etc/envoy/dynamic-modules.

ARG GO_VERSION

FROM golang:${GO_VERSION} AS builder

WORKDIR /workspace

# Tools needed for the cgo build (Coraza WAF pulls in cgo).
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git gcc build-essential binutils

COPY . .
RUN go mod download all

ARG BUILD_TAGS=""

RUN CGO_ENABLED=1 go build -trimpath -buildmode=c-shared ${BUILD_TAGS} -o libcomposer.so .

FROM scratch AS final

COPY --from=builder /workspace/libcomposer* /
