#!/usr/bin/env bash
#
# Deploy the my-filter composer example on an EXISTING cluster (kubectl already
# points at it). Installs Envoy Gateway 1.8.2, applies the EG quickstart
# (GatewayClass/Gateway/HTTPRoute + backend app), then wires the composer
# dynamic module and the my-filter filter.
#
# Env vars:
#   COMPOSER_IMAGE  the composer dynamic-module image to mount. Defaults to the
#                   published Docker Hub image docker.io/zhaohuabing/composer:0.9.0.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EG_VERSION="v1.8.2"
COMPOSER_IMAGE="${COMPOSER_IMAGE:-docker.io/zhaohuabing/composer:0.9.0}"

echo ">> Installing Envoy Gateway ${EG_VERSION}"
helm upgrade --install eg oci://docker.io/envoyproxy/gateway-helm \
  --version "${EG_VERSION}" -n envoy-gateway-system --create-namespace
kubectl -n envoy-gateway-system rollout status deploy/envoy-gateway --timeout=300s

echo ">> Applying the Envoy Gateway ${EG_VERSION} quickstart (Gateway/HTTPRoute/backend)"
kubectl apply -f "https://github.com/envoyproxy/gateway/releases/download/${EG_VERSION}/quickstart.yaml" -n default

echo ">> Applying EnvoyProxy with COMPOSER_IMAGE=${COMPOSER_IMAGE}"
sed "s|__COMPOSER_IMAGE__|${COMPOSER_IMAGE}|g" "${SCRIPT_DIR}/envoyproxy.yaml" | kubectl apply -f -

echo ">> Linking the EnvoyProxy to the eg GatewayClass"
kubectl patch gatewayclass eg --type=merge -p '{
  "spec": {
    "parametersRef": {
      "group": "gateway.envoyproxy.io",
      "kind": "EnvoyProxy",
      "name": "composer-proxy",
      "namespace": "envoy-gateway-system"
    }
  }
}'

echo ">> Applying the EnvoyExtensionPolicy (my-filter filter)"
kubectl apply -f "${SCRIPT_DIR}/envoyextensionpolicy.yaml"

echo ">> Waiting for the Gateway to be programmed (envoy re-rolls out after the parametersRef change)"
kubectl wait --for=condition=Programmed gateway/eg -n default --timeout=300s || true

echo
echo ">> Done. Verify with: ${SCRIPT_DIR}/verify.sh"
