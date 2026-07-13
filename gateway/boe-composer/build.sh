#!/usr/bin/env bash
#
# Build a custom Composer dynamic module (libcomposer.so) OCI image that embeds
# the minimal `my-filter` plugin from ./plugin, and push it to a registry your
# cluster can pull from.
#
# The plugin can't be built inside this repo: every embedded Composer plugin
# shares the single go.mod at built-on-envoy/extensions/composer. So we clone
# built-on-envoy, overlay ./plugin onto extensions/composer/myfilter, replace
# extensions/composer/main/main.go with our ./main/main.go (which registers the
# full upstream plugin set PLUS our my-filter plugin), and let the BoE
# Makefile/Dockerfile do the Go 1.26.x cgo build.
#
# Requirements: git, docker (with buildx), and `docker login` to $BOE_REGISTRY.
# Nothing is compiled on the host directly; the build runs inside Docker.
#
# Env vars:
#   BOE_REF       git ref of built-on-envoy to build from   (default: main)
#   BOE_REGISTRY  registry to push to, e.g. ghcr.io/you      (default: ghcr.io/zhaohuabing)
#   PLATFORMS     buildx target platforms (default: your host arch only, e.g.
#                 linux/arm64). Multi-arch cross-builds emulate under QEMU, which
#                 crashes the Go 1.26 toolchain -- override only with native builders.
#   SANITY        if "true", run `go test`/`go build` in the BoE tree before
#                 the image build (needs a local Go toolchain)  (default: false)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BOE_REF="${BOE_REF:-main}"
BOE_REGISTRY="${BOE_REGISTRY:-ghcr.io/zhaohuabing}"
SANITY="${SANITY:-false}"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

echo ">> Cloning tetratelabs/built-on-envoy @ ${BOE_REF}"
git clone --depth 1 --branch "${BOE_REF}" \
  https://github.com/tetratelabs/built-on-envoy "${WORK_DIR}/boe" 2>/dev/null || \
  git clone https://github.com/tetratelabs/built-on-envoy "${WORK_DIR}/boe"

BOE="${WORK_DIR}/boe"
COMPOSER="${BOE}/extensions/composer"
PLUGIN_DST="${COMPOSER}/myfilter"

echo ">> Overlaying ./plugin -> extensions/composer/myfilter"
mkdir -p "${PLUGIN_DST}/embedded"
cp "${SCRIPT_DIR}/plugin/myfilter.go"       "${PLUGIN_DST}/myfilter.go"
cp "${SCRIPT_DIR}/plugin/myfilter_test.go"  "${PLUGIN_DST}/myfilter_test.go"
cp "${SCRIPT_DIR}/plugin/manifest.yaml"     "${PLUGIN_DST}/manifest.yaml"
cp "${SCRIPT_DIR}/plugin/embedded/host.go"  "${PLUGIN_DST}/embedded/host.go"

echo ">> Overlaying ./main/main.go -> extensions/composer/main/main.go (all BoE plugins + my-filter)"
cp "${SCRIPT_DIR}/main/main.go" "${COMPOSER}/main/main.go"

# Read the composer version straight from its manifest so the printed image ref
# matches what `make` produces ($(HUB)/composer:$(VERSION)).
VERSION="$(sed -ne 's/^version: *//p' "${COMPOSER}/manifest.yaml" | head -n1)"
IMAGE="${BOE_REGISTRY}/composer:${VERSION}"

if [ "${SANITY}" = "true" ]; then
  echo ">> Sanity: go test + go build in the composer module (needs local Go)"
  ( cd "${COMPOSER}" && go test ./myfilter/... && go build -o /dev/null ./main )
fi

# Default to the HOST architecture only. Cross-building the other arch runs the
# Go toolchain under QEMU emulation, which crashes with Go 1.26
# ("fatal error: close of synctest channel from outside bubble") during
# `go mod download`. Building natively avoids that. Override PLATFORMS to go
# multi-arch only if you have a native builder for each arch (e.g. remote nodes).
case "$(uname -m)" in
  arm64|aarch64) HOST_PLATFORM="linux/arm64" ;;
  x86_64|amd64)  HOST_PLATFORM="linux/amd64" ;;
  *)             HOST_PLATFORM="linux/amd64" ;;
esac
PLATFORMS="${PLATFORMS:-${HOST_PLATFORM}}"

echo ">> Building & pushing ${IMAGE} for ${PLATFORMS}"
echo "   (buildx build via the BoE Dockerfile; requires docker login to ${BOE_REGISTRY})"

# The BoE `push_image` target uses `docker buildx --annotation`, which needs
# Buildx >= 0.12. On older buildx we fall back to an equivalent buildx build that
# just drops the (metadata-only) annotations -- the resulting module is identical.
BUILDX_VER="$(docker buildx version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n1 | tr -d v)"
buildx_ge_012() {
  [ -n "${BUILDX_VER}" ] || return 1
  local major minor; major="${BUILDX_VER%%.*}"; minor="${BUILDX_VER#*.}"; minor="${minor%%.*}"
  [ "${major}" -gt 0 ] || [ "${minor}" -ge 12 ]
}

if buildx_ge_012; then
  make -C "${COMPOSER}" push_image BOE_REGISTRY="${BOE_REGISTRY}" PLATFORMS="${PLATFORMS}"
else
  echo "   NOTE: docker buildx ${BUILDX_VER:-<unknown>} < 0.12 (no --annotation); building without annotations."
  GO_VERSION="$(sed -ne 's/^go //p' "${COMPOSER}/go.mod" | head -n1)"
  # Coraza build tags required by the WAF plugin (from extensions/composer/Makefile.common).
  BUILD_TAG="-tags coraza.rule.case_sensitive_args_keys,coraza.rule.no_regex_multiline,coraza.rule.mandatory_rule_id_check"
  # A docker-container builder is needed to push directly to a registry.
  docker buildx inspect boe-builder >/dev/null 2>&1 || \
    docker buildx create --name boe-builder --driver docker-container --use
  docker buildx build --platform="${PLATFORMS}" \
    --builder boe-builder \
    --output type=registry,oci-mediatypes=true \
    --provenance=false \
    --build-arg BUILD_TAG="${BUILD_TAG}" \
    --build-arg LIB_NAME="libcomposer.so" \
    --build-arg GO_VERSION="${GO_VERSION}" \
    --tag "${IMAGE}" \
    -f "${COMPOSER}/Dockerfile" "${COMPOSER}"
fi

echo
echo ">> Done. Set this image in eg/envoyproxy.yaml (spec...volumes[].image.reference):"
echo "     ${IMAGE}"
