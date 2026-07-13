// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0

package myfilter

import (
	"testing"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFactoriesRegisterUnderName checks the plugin advertises itself under the
// name used as filterName in the EnvoyExtensionPolicy.
func TestFactoriesRegisterUnderName(t *testing.T) {
	require.Contains(t, WellKnownHttpFilterConfigFactories(), ExtensionName)
}

// TestOnResponseHeadersStampsHeader drives the filter and asserts the response
// header is set.
func TestOnResponseHeadersStampsHeader(t *testing.T) {
	p := &Plugin{}
	headers := fake.NewFakeHeaderMap(map[string][]string{})

	status := p.OnResponseHeaders(headers, true)

	require.Equal(t, shared.HeadersStatusContinue, status)
	assert.Equal(t, "built-on-envoy", headers.GetOne("x-hello").ToUnsafeString())
}
