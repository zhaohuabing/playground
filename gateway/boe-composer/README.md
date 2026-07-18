# Minimal Composer extension on Envoy Gateway 1.8.2

A minimal example of **extending [Built On Envoy](https://github.com/tetratelabs/built-on-envoy)
`composer` dynamic module** with your own Go HTTP filter, then running it on an
existing **Envoy Gateway 1.8.2** cluster.

The plugin (`my-filter`) does one observable thing: it stamps
`x-hello: built-on-envoy` on every response.

## How it works

Composer is an Envoy **dynamic module** — a c-shared library (`libcomposer.so`)
loaded into the Envoy data plane. Envoy Gateway 1.8 ships Envoy **1.38.0**, which
matches composer `v0.9.0`'s `minEnvoyVersion: 1.38.0`.

This is a **self-contained Go module**: it consumes built-on-envoy's composer
packages as a pinned dependency (`go.mod`) instead of cloning the repo.

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
EnvoyExtensionPolicy → attaches coraza-waf + my-filter to the eg Gateway (in order)
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
  envoyextensionpolicy.yaml  # attaches coraza-waf + my-filter to the eg Gateway (in order)
  install.sh                 # helm install EG 1.8.2 + quickstart + wiring
  verify.sh                  # normal req passes & is stamped; SQLi is blocked by the WAF
```

## Prerequisites

- An existing Kubernetes cluster (`kubectl` configured) whose nodes can pull your image.
- **Kubernetes 1.35+** — the `EnvoyProxy` mounts the module as an OCI **image volume**,
  which Envoy Gateway only renders on k8s 1.35 or newer. On older clusters the volume
  is silently dropped and Envoy fails with `libcomposer.so: cannot open shared object
  file`. See [Notes & caveats](#notes--caveats) for the workaround on < 1.35.
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

The `EnvoyExtensionPolicy` chains **both** composer filters, so every request flows
through them in order:

```
request ──▶ coraza-waf (OWASP CRS, blocking) ──▶ my-filter ──▶ backend
```

Expected from `verify.sh`:
- a normal request → `200` with `x-hello: built-on-envoy` (allowed by the WAF, stamped by my-filter);
- a SQL-injection request → `403 Forbidden` (blocked by the WAF). The `x-hello` header is
  still present on the 403, which shows the response traversed my-filter too.

Both filters are just two entries in one policy — same `name: composer` (they share the
embedded `libcomposer.so`), differing only by `filterName`; `dynamicModule` is an ordered
list, so their position sets the filter-chain order. To run my-filter alone, drop the
`coraza-waf` entry from `eg/envoyextensionpolicy.yaml`.

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
- **Requires Kubernetes 1.35+ for the image-volume mount.** EG only renders the
  `envoyDeployment.pod.volumes[].image` OCI image volume on k8s **1.35+**. On older
  clusters (verified on 1.33 and 1.34) EG omits the volume entirely, so
  `/etc/envoy/dynamic-modules/libcomposer.so` is missing and Envoy NACKs the listener
  with `Failed to load dynamic module ... cannot open shared object file`. Confirm with:
  ```bash
  kubectl version | grep Server
  kubectl -n envoy-gateway-system get deploy \
    -l gateway.envoyproxy.io/owning-gateway-name=eg \
    -o jsonpath='{.items[0].spec.template.spec.volumes[?(@.name=="dynamic-modules")]}'
  # empty output => EG didn't render the image volume => cluster is < 1.35
  ```
  **Workaround for < 1.35:** replace the image volume with an `emptyDir` + an
  `initContainer` that copies `/libcomposer.so` into it. That needs the composer image
  to carry `cp` (the default image is `FROM scratch`), so rebuild it on a minimal base
  such as `busybox` first.
- **Dynamic Modules is experimental in EG 1.8.** If `EnvoyProxy.spec.dynamicModules`
  is rejected, your cluster's EG CRDs predate 1.8.2 — `install.sh` upgrades the chart,
  but a stale separate CRD install may need refreshing.
- **Image must be pullable by cluster nodes** (public repo or an imagePullSecret).
- **Want a leaner module?** `main.go` blank-imports the whole upstream plugin set
  plus `my-filter`. Go only compiles what's imported, so removing e.g. the WAF/OPA/SAML
  imports skips their (heavy) dependencies entirely.

## References

- Reference plugin example: <https://github.com/tetratelabs/built-on-envoy/tree/main/extensions/composer/example>
- Envoy Gateway dynamic modules: <https://gateway.envoyproxy.io/v1.8/tasks/extensibility/dynamic-modules/>
