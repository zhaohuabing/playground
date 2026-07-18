# Build & push the custom Composer dynamic module (libcomposer.so) as an OCI
# image.

HUB               ?= docker.io/zhaohuabing
NAME              ?= composer
# Keep in sync with the built-on-envoy/extensions/composer version pinned in go.mod.
TAG               ?= 0.9.0
IMAGE             ?= $(HUB)/$(NAME):$(TAG)
PLATFORMS         ?= linux/amd64,linux/arm64
REGISTRY_INSECURE ?= false
GO_VERSION        := $(shell sed -ne 's/^go //p' go.mod)

# Coraza build tags required by the WAF plugin (from BOE extensions/composer/Makefile.common).
BUILD_TAGS_CORAZA := coraza.rule.case_sensitive_args_keys,coraza.rule.no_regex_multiline,coraza.rule.mandatory_rule_id_check

default: build

##@ Build

.PHONY: build
build:  ## Build libcomposer.so for the local platform
	CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -tags $(BUILD_TAGS_CORAZA) -o libcomposer.so .

.PHONY: test
test:  ## Run tests
	TEST_GODEBUG=cgocheck=0 go test ./...

.PHONY: clean
clean:  ## Remove build artifacts
	rm -f libcomposer.so libcomposer.h

##@ Docker

.PHONY: build_image
build_image:  ## Build the composer image for the local platform (loads into docker, no push)
	docker buildx build \
		--output type=docker,oci-mediatypes=true \
		--provenance=false \
		--build-arg BUILD_TAGS="-tags $(BUILD_TAGS_CORAZA)" \
		--build-arg GO_VERSION="$(GO_VERSION)" \
		-t $(IMAGE) \
		-f ./Dockerfile .

registry_output :=
ifeq ($(REGISTRY_INSECURE),true)
  registry_output := ",registry.insecure=true"
endif

.PHONY: push_image
push_image: create_builder  ## Build a multi-arch image and push it to the registry
	docker buildx build --platform=$(PLATFORMS) \
		--builder $(BUILDER_NAME) \
		--output type=registry,oci-mediatypes=true$(registry_output) \
		--provenance=false \
		--build-arg BUILD_TAGS="-tags $(BUILD_TAGS_CORAZA)" \
		--build-arg GO_VERSION="$(GO_VERSION)" \
		--tag $(IMAGE) \
		-f ./Dockerfile .

BUILDER_NAME := composer-builder
.PHONY: create_builder
create_builder:
	@docker buildx inspect $(BUILDER_NAME) || \
		docker buildx create --name $(BUILDER_NAME) \
			--driver docker-container --driver-opt network=host \
			--buildkitd-flags '--allow-insecure-entitlement network.host' --use

##@ Help

.PHONY: help
help:  ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
