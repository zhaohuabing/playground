// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0

// Package main builds the Composer shared library (libcomposer.so). It registers
// the required dynamic-module ABI, all Built-On-Envoy composer plugins (consumed
// as a pinned Go module dependency), and our custom my-filter example plugin.
//
// This module is self-contained: `go build -buildmode=c-shared` pulls composer
// from the Go module proxy — no built-on-envoy checkout or file overlay needed.
package main

import (
	_ "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/abi"

	// All Built-On-Envoy composer plugins (mirrors extensions/composer/plugins.go).
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/anthropic-decoder/embedded"        // Anthropic Decoder plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/azure-content-safety/embedded"     // Azure Content Safety plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/bedrock-guardrails/embedded"       // Bedrock Guardrails plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/cedar/embedded"                    // Cedar authorization plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/chat-completions-decoder/embedded" // Chat Completions Decoder plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/cluster-router/embedded"           // Cluster Router plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/example/embedded"                  // Example built-in plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/file-server/embedded"              // File server plugin.
	// Go plugin loader for composer plugins compiled into separate shared libraries.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/goplugin-loader"
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/jwe-decrypt/embedded"       // JWE decryption plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/llm-proxy/embedded"         // LLM Proxy plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/opa/embedded"               // OPA authorization plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/openapi-validator/embedded" // OpenAPI validator plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/openfga/embedded"           // OpenFGA authorization plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/saml/embedded"              // SAML SP plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/token-exchange/embedded"    // OAuth2 Token Exchange plugin.
	_ "github.com/tetratelabs/built-on-envoy/extensions/composer/waf/embedded"               // WAF plugin.

	// Our custom example plugin.
	_ "github.com/zhaohuabing/boe-composer/myfilter/embedded" // my-filter example plugin.
)

func main() {} // main is required to build as a C shared library.
