# connect-tunnel: ZTG-style CONNECT tunnels on stock Envoy Gateway

Prototype of the Zero Trust Gateway data path using only EG resources.
Proves that stock EG can already do:

| Test (all over HTTP/2, stock listener) | Result |
|------|--------|
| No device cert | TLS handshake rejected at the listener |
| Device cert, no token | CONNECT rejected, 401 — the layers are independent |
| Device cert, token for another target | CONNECT rejected, 403 (per-route claim pinning) |
| Device cert + valid token | tunnel established 200, echo reached through it |

Everything runs over HTTP/2 on a stock listener. HTTP/1.1 CONNECT also works
but isn't exercised; the target model is one multiplexed h2 connection per
client.

The design point this validates: per-stream authentication with
controller-minted JWTs validated natively by Envoy (SecurityPolicy), instead
of a per-connection ext_authz callout — the "controller off the data path"
variant. A standalone static-config Envoy stands in for the Tunnel-Controller's
JWKS endpoint; `scripts/mint-jwt.sh` stands in for its token-minting API.

## Topology

```mermaid
flowchart LR
  subgraph ct["ns: connect-tunnel"]
    Client["client pod (curl)<br/>= Tunnel-Client + app<br/>device cert + short-lived JWT"]
    Echo["echo :8080<br/>tunnel target"]
    JWKS["jwks: static Envoy<br/>direct_response /jwks.json"]
  end

  subgraph egs["ns: envoy-gateway-system"]
    Proxy["EG-managed Envoy, HTTPS :443<br/>1. mTLS vs client CA (CTP)<br/>2. JWT sig + target claim (SecurityPolicy)<br/>3. CONNECT terminate (HTTPRoute + BTP)"]
    EG["Envoy Gateway<br/>control plane"]
  end

  Client -- "TLS + mTLS over h2<br/>CONNECT echo.internal.example:8080<br/>Proxy-Authorization: Bearer JWT per stream" --> Proxy
  Proxy -- "spliced raw TCP" --> Echo
  Proxy -. "async JWKS fetch" .-> JWKS
  EG -. "xDS" .-> Proxy
```

Solid arrows are the data path; dotted arrows never sit on it — the JWKS fetch
is asynchronous and xDS is control plane, which is the point of the
native-JWT-instead-of-ext_authz variant.

## Layout

- `manifests/10-backend.yaml` — echo server, the tunnel target
- `manifests/15-client.yaml` — curl pod standing in for the Tunnel-Client
- `manifests/20-jwks-server.yaml` — standalone Envoy (static config,
  `direct_response`) serving the generated JWKS
- `manifests/30-gateway.yaml` — HTTPS listener (no hostname: clients dial by
  address, tunnel targets are matched per-route from the CONNECT authority)
- `manifests/40-route-btp.yaml` — HTTPRoute (one per target) + BTP
  `httpUpgrade: CONNECT, terminate: true`
- `manifests/50-securitypolicy.yaml` — JWT from `Proxy-Authorization`,
  authorization rule pinning the `target` claim to this route's target
- `manifests/70-ctp-mtls.yaml` — ClientTrafficPolicy requiring client certs
  from the generated client CA (phase 3, cert-per-device)

## Run

Prereqs: a cluster with EG >= v1.5.0 (CONNECT terminate; this was built against
a gateway-dev image).

```sh
scripts/run.sh   # deploy everything: gateway, backend, JWKS, CONNECT, JWT, mTLS
scripts/test.sh  # full matrix over HTTP/2: TLS reject / 401 / 403 / 200
```

`scripts/mint-jwt.sh [target] [ttl]` mints tokens; default target is
`echo.internal.example:8080`, default TTL 600s.

## Example run

```text
$ scripts/test.sh
--- A) no device cert (expect TLS alert: certificate required)
curl: (56) OpenSSL SSL_read: OpenSSL/3.3.2: error:0A00045C:SSL routines::tlsv13 alert certificate required, errno 0
gateway: no auth-filter denial — rejected at the TLS layer
--- B) cert, no token (expect 401 from the jwt filter)
curl: (56) Failure when receiving data from the peer
gateway: denied by the jwt filter (401)
--- C) cert, token for another target (expect 403 from authorization)
curl: (56) Failure when receiving data from the peer
gateway: denied by the authorization policy (403)
--- D) cert + valid token (expect established, response 200 + echo body)
* CONNECT tunnel: HTTP/2 negotiated
* CONNECT tunnel established, response 200
 "host": "echo.internal.example:8080",
 "proto": "HTTP/1.1",
```
