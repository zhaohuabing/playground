#!/usr/bin/env bash
# Generate the proxy TLS cert, the JWT signing key, and the JWKS, then load
# them into the cluster (Secret proxy-tls, ConfigMap jwks). Idempotent.
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p secrets

if [ ! -f secrets/proxy.crt ]; then
  openssl req -x509 -newkey rsa:2048 -keyout secrets/proxy.key -out secrets/proxy.crt \
    -days 30 -nodes -subj "/CN=proxy.internal.example" \
    -addext "subjectAltName=DNS:proxy.internal.example"
fi

if [ ! -f secrets/jwt.key ]; then
  openssl genrsa -out secrets/jwt.key 2048
fi

# Phase 3 (mTLS): client CA plus one "device" cert signed by it.
if [ ! -f secrets/client-ca.crt ]; then
  openssl req -x509 -newkey rsa:2048 -keyout secrets/client-ca.key -out secrets/client-ca.crt \
    -days 30 -nodes -subj "/CN=tunnel-client-ca"
fi
if [ ! -f secrets/client.crt ]; then
  openssl req -newkey rsa:2048 -keyout secrets/client.key -out secrets/client.csr \
    -nodes -subj "/CN=device-robin"
  openssl x509 -req -in secrets/client.csr -CA secrets/client-ca.crt \
    -CAkey secrets/client-ca.key -CAcreateserial -out secrets/client.crt -days 7
fi

python3 - <<'EOF'
import base64, json, subprocess

out = subprocess.check_output(
    ["openssl", "rsa", "-in", "secrets/jwt.key", "-noout", "-modulus"]).decode()
nhex = out.strip().split("=")[1]
n = base64.urlsafe_b64encode(bytes.fromhex(nhex)).rstrip(b"=").decode()
e = base64.urlsafe_b64encode((65537).to_bytes(3, "big")).rstrip(b"=").decode()
jwks = {"keys": [{"kty": "RSA", "alg": "RS256", "use": "sig",
                  "kid": "tunnel-key", "n": n, "e": e}]}
with open("secrets/jwks.json", "w") as f:
    json.dump(jwks, f)
print("wrote secrets/jwks.json")
EOF

kubectl create secret tls proxy-tls -n connect-tunnel \
  --cert=secrets/proxy.crt --key=secrets/proxy.key \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl create configmap jwks -n connect-tunnel \
  --from-file=jwks.json=secrets/jwks.json \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl create configmap client-ca -n connect-tunnel \
  --from-file=ca.crt=secrets/client-ca.crt \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret tls client-cert -n connect-tunnel \
  --cert=secrets/client.crt --key=secrets/client.key \
  --dry-run=client -o yaml | kubectl apply -f -
