// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0

// Package host registers the myfilter plugin into the Composer host binary. It
// is blank-imported from main/main.go so that the plugin is compiled directly
// into libcomposer.so (the "embedded" packaging approach).
package host

import (
	sdk "github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go"

	myfilter "github.com/tetratelabs/built-on-envoy/extensions/composer/myfilter"
)

func init() {
	sdk.RegisterHttpFilterConfigFactories(myfilter.WellKnownHttpFilterConfigFactories())
}
