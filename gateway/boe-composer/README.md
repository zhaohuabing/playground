# Minimal Composer extension on Envoy Gateway 1.8.2

A minimal example of **extending Tetrate's [Built On Envoy](https://github.com/tetratelabs/built-on-envoy)
`composer` dynamic module** with your own Go HTTP filter, then running it on an
existing **Envoy Gateway 1.8.2** cluster.

The plugin (`my-filter`) does one observable thing: it stamps
`x-hello: built-on-envoy` on every response.

## How it works

Composer is an Envoy **dynamic module** — a c-shared library (`libcomposer.so`)
loaded into the Envoy data plane. Envoy Gateway 1.8 ships Envoy **1.38.0**, which
matches composer `v0.9.0`'s `minEnvoyVersion: 1.38.0`.

This is a **self-contained Go module**: it consumes built-on-envoy's composer
packages as a pinned dependency (`go.mod`) instead of cloning the repo. It follows
the same pattern as [tetrateio/envoy-dynamic-modules/teg](https://github.com/tetrateio/envoy-dynamic-modules/blob/main/teg/Makefile)
(minus FIPS), so `go build -buildmode=c-shared` pulls everything from the Go module
proxy — no checkout, no file overlay.

```
myfilter/myfilter.go ─ OnResponseHeaders stamps a header
       │               (uses only the public dynamic-modules Go SDK)
       ▼
myfilter/embedded/host.go ─ init() registers my-filter with the composer SDK
       ▼
main.go ─────────── blank-imports all BoE composer plugins + my-filter + the ABI
       │            → compiled into libcomposer.so
       ▼
make push_image ─── builds & pushes the OCI image via the local Dockerfile
       ▼
EnvoyProxy ───────── mounts the image, loads /etc/envoy/dynamic-modules/libcomposer.so
EnvoyExtensionPolicy → attaches filterName: my-filter to the eg Gateway
```

Our `main.go` registers the full upstream plugin set (Coraza WAF, OPA, Cedar,
SAML, ...) **plus** our `my-filter` plugin, so the resulting module is a drop-in
superset of upstream composer.

## Layout

```
go.mod / go.sum     # pins built-on-envoy/extensions/composer v0.9.0 + the SDK
main.go             # libcomposer.so entrypoint — registers all BoE plugins + my-filter + ABI
myfilter/           # the custom plugin (its own package in THIS module)
  myfilter.go       #   package myfilter — the HTTP filter (public SDK only)
  myfilter_test.go  #   unit test
  embedded/host.go  #   init() registers the plugin with the SDK
  manifest.yaml     #   plugin metadata (name: my-filter)
Dockerfile          # cgo c-shared build → OCI image with libcomposer.so at root
Makefile            # build / test / build_image / push_image (adapted from teg, non-FIPS)
eg/
  envoyproxy.yaml            # mounts the image + declares the dynamic module
  envoyextensionpolicy.yaml  # attaches filterName: my-filter to the eg Gateway
  install.sh                 # helm install EG 1.8.2 + quickstart + wiring
  verify.sh                  # curl through the Gateway, assert the header
```

## Prerequisites

- An existing Kubernetes cluster (`kubectl` configured) whose nodes can pull your image.
- `helm`, `docker` (with `buildx`), `curl`, and `make`.
- Push access to a registry (`docker login`) — e.g. GHCR.
- No local Go toolchain is required for the image build (Go is used inside the
  builder). For `make build`/`make test` locally you need Go 1.26.x.

## 1. Build & push the custom composer image

```bash
cd gateway/boe-composer
make push_image                       # defaults to HUB=ghcr.io/zhaohuabing, TAG=0.9.0
# override: make push_image HUB=ghcr.io/<you> TAG=<tag>
# single arch / local load only:  make build_image PLATFORMS=linux/arm64
```

`push_image` builds a multi-arch image and pushes `$(HUB)/composer:$(TAG)`
(default `ghcr.io/zhaohuabing/composer:0.9.0`). Make sure the repo is pullable by
the cluster (public, or add an imagePullSecret).

To build/test locally without Docker:

```bash
make test     # go test ./...
make build    # produces libcomposer.so for your host platform
```

## 2. Deploy on the cluster

```bash
COMPOSER_IMAGE=ghcr.io/zhaohuabing/composer:0.9.0 ./eg/install.sh
```

This installs Envoy Gateway 1.8.2, applies the EG quickstart (Gateway `eg`, an
HTTPRoute for `www.example.com`, and a backend app), applies the `EnvoyProxy`
(mounting your image), links it to the `eg` GatewayClass, and applies the
`EnvoyExtensionPolicy`.

## 3. Verify

```bash
./eg/verify.sh
```

Expected: HTTP 200 with `x-hello: built-on-envoy`. The script also prints debug
hints (policy status, proxy logs) on failure.

To confirm from the module side:

```bash
kubectl get envoyextensionpolicy my-filter-extension -o yaml   # expect Accepted / Programmed
kubectl -n envoy-gateway-system logs -l gateway.envoyproxy.io/owning-gateway-name=eg -c envoy | grep -i dynamic
```

## Notes & caveats

- **Version alignment.** `go.mod` pins `built-on-envoy/extensions/composer v0.9.0`
  and the matching `dynamic_modules` SDK version; the Makefile `TAG` mirrors it. To
  move to a newer composer, bump both the `require` in `go.mod` (run `go mod tidy`)
  and `TAG` in the Makefile, and confirm the new `minEnvoyVersion` still matches
  your EG's Envoy version.
- **Dynamic Modules is experimental in EG 1.8.** If `EnvoyProxy.spec.dynamicModules`
  is rejected, your cluster's EG CRDs predate 1.8.2 — `install.sh` upgrades the chart,
  but a stale separate CRD install may need refreshing.
- **Image must be pullable by cluster nodes** (public repo or an imagePullSecret).
- **Want a leaner module?** `main.go` blank-imports the whole upstream plugin set
  plus `my-filter`. Go only compiles what's imported, so removing e.g. the WAF/OPA/SAML
  imports skips their (heavy) dependencies entirely.

## References

- teg (the pattern this follows): <https://github.com/tetrateio/envoy-dynamic-modules/blob/main/teg/Makefile>
- Reference plugin example: <https://github.com/tetratelabs/built-on-envoy/tree/main/extensions/composer/example>
- Envoy Gateway dynamic modules: <https://gateway.envoyproxy.io/v1.8/tasks/extensibility/dynamic-modules/>
