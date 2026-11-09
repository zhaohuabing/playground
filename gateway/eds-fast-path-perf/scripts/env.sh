# Settings shared by the scripts. Override any of them in the environment.
: "${CLUSTER:=eds-perf}"
: "${CONTEXT:=kind-${CLUSTER}}"
: "${EG_SRC:=$HOME/workspace/eg-fastpath}"   # checkout of the PR branch
: "${EG_IMAGE:=docker.io/envoyproxy/gateway-dev}"
: "${EG_TAG:=fastpath-44a6cf5-mfix}"
: "${EG_NS:=envoy-gateway-system}"
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$HERE")"
k() { kubectl --context "$CONTEXT" "$@"; }
