// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0

// Package myfilter is a minimal example Composer plugin: an HTTP filter that
// stamps a single response header so a successful load is observable end-to-end.
//
// It depends only on the public dynamic-modules Go SDK, so it lives in this
// module and is compiled into libcomposer.so by blank-importing ./embedded from
// the root main.go — no built-on-envoy checkout or overlay required.
package myfilter

import shared "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

// ExtensionName is the name this plugin registers under; it is the value used as
// `filterName` in the EnvoyExtensionPolicy that references this filter.
const ExtensionName = "my-filter"

// 1) the filter — one callback per response.
type Plugin struct{ shared.EmptyHttpFilter }

func (p *Plugin) OnResponseHeaders(h shared.HeaderMap, end bool) shared.HeadersStatus {
	h.Set("x-hello", "built-on-envoy")
	return shared.HeadersStatusContinue
}

// 2) per-stream + per-config factories.
// Factory embeds EmptyHttpFilterFactory to satisfy the interface (it supplies
// OnDestroy); Create returns a fresh Plugin per stream.
type Factory struct{ shared.EmptyHttpFilterFactory }

func (f *Factory) Create(h shared.HttpFilterHandle) shared.HttpFilter { return &Plugin{} }

type ConfigFactory struct{ shared.EmptyHttpFilterConfigFactory }

func (c *ConfigFactory) Create(h shared.HttpFilterConfigHandle, cfg []byte) (shared.HttpFilterFactory, error) {
	return &Factory{}, nil
}

// 3) advertise the extension by name.
func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return map[string]shared.HttpFilterConfigFactory{
		ExtensionName: &ConfigFactory{},
	}
}
