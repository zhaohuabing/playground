// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0

// Package main builds my-filter as a standalone Go plugin that the composer
// dynamic module can load at runtime via its goplugin-loader. This is the
// packaging `boe run --local ./myfilter` builds and loads.
//
// The exported WellKnownHttpFilterConfigFactories is the entrypoint the loader
// looks up; it just delegates to the shared myfilter implementation, so the
// embedded (main.go) and standalone builds register the exact same filter.
package main

import (
	shared "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"

	impl "github.com/zhaohuabing/boe-composer/myfilter"
)

func WellKnownHttpFilterConfigFactories() map[string]shared.HttpFilterConfigFactory { //nolint:revive
	return impl.WellKnownHttpFilterConfigFactories()
}
