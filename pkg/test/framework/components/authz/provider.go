// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package authz

import (
	"fmt"
	"sort"
	"strings"

	"istio.io/istio/pkg/config/protocol"
	echoClient "istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/match"
)

// API used by a Provider. Either HTTP or GRPC.
type API string

const (
	HTTP API = "http"
	GRPC API = "grpc"
)

// Provider for authz requests.
type Provider interface {
	Name() string

	// API used by this provider.
	API() API

	// IsProtocolSupported returns true if the given request protocol is supported by this provider.
	IsProtocolSupported(protocol.Instance) bool

	// IsTargetSupported returns true if the given target is supported by this provider.
	IsTargetSupported(target echo.Target) bool

	// MatchSupportedTargets returns a Matcher for filtering unsupported targets.
	MatchSupportedTargets() match.Matcher

	// Check returns an echo.Checker for validating response based on the request information.
	Check(opts echo.CallOptions, expectAllowed bool) echo.Checker
}

var _ Provider = &providerImpl{}

type providerImpl struct {
	name              string
	api               API
	protocolSupported func(protocol.Instance) bool
	targetSupported   func(echo.Target) bool
	check             func(opts echo.CallOptions, expectAllowed bool) echo.Checker
}

func (p *providerImpl) Name() string {
	return p.name
}

func (p *providerImpl) API() API {
	return p.api
}

func (p *providerImpl) IsProtocolSupported(i protocol.Instance) bool {
	return p.protocolSupported(i)
}

func (p *providerImpl) IsTargetSupported(to echo.Target) bool {
	return p.targetSupported(to)
}

func (p *providerImpl) MatchSupportedTargets() match.Matcher {
	return func(i echo.Instance) bool {
		return p.IsTargetSupported(i)
	}
}

func (p *providerImpl) Check(opts echo.CallOptions, expectAllowed bool) echo.Checker {
	return p.check(opts, expectAllowed)
}

func checkHTTP(opts echo.CallOptions, expectAllowed bool) echo.Checker {
	override := opts.HTTP.Headers.Get(XExtAuthzAdditionalHeaderOverride)
	var hType echoClient.HeaderType
	if expectAllowed {
		hType = echoClient.RequestHeader
	} else {
		hType = echoClient.ResponseHeader
	}
	headerChecker := check.And(
		headerContains(hType, map[string][]string{
			XExtAuthzCheckReceived:            {"additional-header-new-value", "additional-header-override-value"},
			XExtAuthzAdditionalHeaderOverride: {"additional-header-override-value"},
		}),
		headerNotContains(hType, map[string][]string{
			XExtAuthzCheckReceived:            {override},
			XExtAuthzAdditionalHeaderOverride: {override},
		}))

	return checkRequest(opts, expectAllowed, headerChecker)
}

func checkGRPC(opts echo.CallOptions, expectAllowed bool) echo.Checker {
	override := opts.HTTP.Headers.Get(XExtAuthzAdditionalHeaderOverride)
	var hType echoClient.HeaderType
	if expectAllowed {
		hType = echoClient.RequestHeader
	} else {
		hType = echoClient.ResponseHeader
	}
	checkHeaders := check.And(
		headerContains(hType, map[string][]string{
			XExtAuthzCheckReceived:            {override},
			XExtAuthzAdditionalHeaderOverride: {GRPCAdditionalHeaderOverrideValue},
		}),
		headerNotContains(hType, map[string][]string{
			XExtAuthzAdditionalHeaderOverride: {override},
		}))

	return checkRequest(opts, expectAllowed, checkHeaders)
}

func checkRequest(opts echo.CallOptions, expectAllowed bool, checkHeaders echo.Checker) echo.Checker {
	switch {
	case opts.Port.Protocol.IsGRPC():
		switch opts.HTTP.Headers.Get(XExtAuthz) {
		case XExtAuthzAllow:
			return check.And(check.NoError(), checkHeaders)
		default:
			// Deny
			return check.And(check.Forbidden(protocol.GRPC),
				check.ErrorContains("desc = denied by ext_authz for not found header "+
					"`x-ext-authz: allow` in the request"))
		}
	case opts.Port.Protocol.IsTCP():
		if expectAllowed {
			return check.NoError()
		}
		// Deny
		return check.Forbidden(protocol.TCP)
	default:
		// HTTP
		switch opts.HTTP.Headers.Get(XExtAuthz) {
		case XExtAuthzAllow:
			return check.And(check.NoError(), checkHeaders)
		default:
			// Deny
			return check.And(check.Forbidden(protocol.HTTP), checkHeaders)
		}
	}
}

func headerContains(hType echoClient.HeaderType, expected map[string][]string) echo.Checker {
	return check.Each(func(r echoClient.Response) error {
		h := r.GetHeaders(hType)
		for _, key := range sortKeys(expected) {
			actual := h.Get(key)

			for _, value := range expected[key] {
				if !strings.Contains(actual, value) {
					return fmt.Errorf("status code %s, expected %s header `%s` to contain `%s`, value=`%s`, raw content=%s",
						r.Code, hType, key, value, actual, r.RawContent)
				}
			}
		}
		return nil
	})
}

func headerNotContains(hType echoClient.HeaderType, expected map[string][]string) echo.Checker {
	return check.Each(func(r echoClient.Response) error {
		h := r.GetHeaders(hType)
		for _, key := range sortKeys(expected) {
			actual := h.Get(key)

			for _, value := range expected[key] {
				if strings.Contains(actual, value) {
					return fmt.Errorf("status code %s, expected %s header `%s` to not contain `%s`, value=`%s`, raw content=%s",
						r.Code, hType, key, value, actual, r.RawContent)
				}
			}
		}
		return nil
	})
}

func sortKeys(v map[string][]string) []string {
	out := make([]string, 0, len(v))
	for k := range v {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
