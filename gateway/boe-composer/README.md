# Minimal Composer extension on Envoy Gateway 1.8.2

A minimal example of **extending Tetrate's [Built On Envoy](https://github.com/tetratelabs/built-on-envoy)
`composer` dynamic module** with your own Go HTTP filter, then running it on an
existing **Envoy Gateway 1.8.2** cluster.

The plugin (`my-filter`) does one observable thing: it stamps
`x-hello: built-on-envoy` on every response.

## How it works

Composer is an Envoy **dynamic module** — a c-shared library (`libcomposer.so`)
loaded into the Envoy data plane. Envoy Gateway 1.8 ships Envoy **1.38.0**, which
matches composer's `minEnvoyVersion: 1.38.0`, so a module built from Built-On-Envoy
`main` is compatible.

```
plugin/myfilter.go ─ OnResponseHeaders stamps a header
       │             (implements the shared.HttpFilter API, like BoE's example plugin)
       ▼
main/main.go ─────── registers all in-tree BoE plugins + my-filter + the ABI
       │             → compiled into libcomposer.so
       ▼
build.sh ─────────── clones built-on-envoy, overlays plugin/ + main/, builds &
       │             pushes the OCI image via the BoE Makefile/Dockerfile
       ▼
EnvoyProxy ───────── mounts the image, loads /etc/envoy/dynamic-modules/libcomposer.so
EnvoyExtensionPolicy → attaches filterName: my-filter to the eg Gateway
```

We use the **embedded** packaging approach (plugin compiled into the module), which
guarantees Go-runtime/dependency compatibility. Our custom `main/main.go` registers the
full upstream plugin set (Coraza WAF, OPA, Cedar, SAML, ...) **plus** our `my-filter`
plugin, so the resulting module is a drop-in superset of upstream composer.

## Layout

```
plugin/             # the custom plugin source (overlaid onto built-on-envoy at build time)
  myfilter.go       #   package myfilter — the HTTP filter
  myfilter_test.go  #   unit test (mirrors BoE's example_test.go)
  embedded/host.go  #   registers the plugin with the SDK
  manifest.yaml     #   plugin metadata (name: my-filter)
main/main.go        # libcomposer.so entrypoint — registers all BoE plugins + my-filter + ABI
build.sh            # clone BoE, overlay, build & push the OCI image
eg/
  envoyproxy.yaml            # mounts the image + declares the dynamic module
  envoyextensionpolicy.yaml  # attaches filterName: my-filter to the eg Gateway
  install.sh                 # helm install EG 1.8.2 + quickstart + wiring
  verify.sh                  # curl through the Gateway, assert the header
```

## Prerequisites

- An existing Kubernetes cluster (`kubectl` configured) whose nodes can pull your image.
- `helm`, `docker` (with `buildx`), `git`, `curl`. Buildx ≥ 0.12 uses BoE's
  `make push_image`; on older buildx `build.sh` falls back to a plain buildx push
  (drops metadata-only OCI annotations — the module is identical).
- Push access to a registry (`docker login`) — e.g. GHCR. The BoE build runs entirely
  in Docker, so no local Go toolchain is required (Go 1.26.x is used inside the builder).

## 1. Build & push the custom composer image

```bash
cd gateway/boe-composer
./build.sh                       # defaults to BOE_REGISTRY=ghcr.io/zhaohuabing
# override: BOE_REGISTRY=ghcr.io/<you> ./build.sh
# optional: BOE_REF=<tag/branch> and SANITY=true (runs go test/build if you have Go locally)
```

`build.sh` prints the resulting image reference, e.g. `ghcr.io/zhaohuabing/composer:0.10.0-dev`.
Make sure the repo is pullable by the cluster (public, or add an imagePullSecret).

## 2. Deploy on the cluster

```bash
COMPOSER_IMAGE=ghcr.io/zhaohuabing/composer:0.10.0-dev ./eg/install.sh
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

- **Dynamic Modules is experimental in EG 1.8.** If `EnvoyProxy.spec.dynamicModules`
  is rejected, your cluster's EG CRDs predate 1.8.2 — `install.sh` upgrades the chart,
  but a stale separate CRD install may need refreshing.
- **Image must be pullable by cluster nodes** (public repo or an imagePullSecret).
- **Build for your host arch.** `build.sh` defaults `PLATFORMS` to the host
  architecture, because cross-building the other arch runs the Go 1.26 toolchain
  under QEMU, which crashes (`close of synctest channel from outside bubble`).
  If your cluster nodes are a different arch, build on a native node of that arch
  (or a remote builder) and set `PLATFORMS=linux/amd64` etc.
- **Want a leaner module?** `main/main.go` mirrors upstream `plugins.go` and adds `my-filter`.
  To build a smaller `libcomposer.so`, drop the plugins you don't need from the import
  list — Go only compiles what's imported, so removing e.g. the WAF/OPA/SAML imports
  skips their (heavy) dependencies entirely.

## References

- Reference example: <https://github.com/tetratelabs/built-on-envoy/tree/main/extensions/composer/example>
- Envoy Gateway dynamic modules: <https://gateway.envoyproxy.io/v1.8/tasks/extensibility/dynamic-modules/>
