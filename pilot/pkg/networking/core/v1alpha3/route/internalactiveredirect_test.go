package route

import (
	"reflect"
	"testing"

	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"
)

func TestGenerateActiveRedirectPolicyWithPolicies(t *testing.T) {
	testInput := &networking.HTTPInternalActiveRedirect{
		Policies: []*networking.HTTPInternalActiveRedirect_RedirectPolicy{
			{
				MaxInternalRedirects:  3,
				RedirectResponseCodes: []uint32{500, 501},
				RedirectUrlRewriteSpecifier: &networking.HTTPInternalActiveRedirect_RedirectPolicy_RedirectUrl{
					RedirectUrl: "www.youku.com",
				},
			},
			{
				MaxInternalRedirects:  3,
				RedirectResponseCodes: []uint32{400, 401},
				RedirectUrlRewriteSpecifier: &networking.HTTPInternalActiveRedirect_RedirectPolicy_RedirectUrl{
					RedirectUrl: "www.taobao.com",
				},
			},
		},
	}

	regexEngine := &matcher.RegexMatcher_GoogleRe2{GoogleRe2: &matcher.RegexMatcher_GoogleRE2{
		MaxProgramSize: &wrappers.UInt32Value{
			Value: uint32(10),
		},
	}}

	outPolicy := TranslateInternalActiveRedirectPolicy(testInput, regexEngine)
	if outPolicy == nil {
		t.Errorf("Failed to generate redirection policy")
	}

	if len(outPolicy.Policies) != len(testInput.Policies) {
		t.Errorf("The expectation is to generate %d redirection policies, which is actually %d", len(outPolicy.Policies), len(testInput.Policies))
	}

	expectedCodes := []uint32{500, 501}
	if !reflect.DeepEqual(outPolicy.Policies[0].RedirectResponseCodes, expectedCodes) {
		t.Errorf("The expected response code is %v, which is actually %d", expectedCodes, outPolicy.Policies[0].RedirectResponseCodes)
	}
}

func TestGenerateActiveRedirectPolicy(t *testing.T) {
	testInput := &networking.HTTPInternalActiveRedirect{
		MaxInternalRedirects:  3,
		RedirectResponseCodes: []uint32{500, 501},
		RedirectUrlRewriteSpecifier: &networking.HTTPInternalActiveRedirect_RedirectUrl{
			RedirectUrl: "www.taobao.com",
		},
	}

	regexEngine := &matcher.RegexMatcher_GoogleRe2{GoogleRe2: &matcher.RegexMatcher_GoogleRE2{
		MaxProgramSize: &wrappers.UInt32Value{
			Value: uint32(10),
		},
	}}

	outPolicy := TranslateInternalActiveRedirectPolicy(testInput, regexEngine)
	if outPolicy == nil {
		t.Errorf("Failed to generate redirection policy")
	}

	if outPolicy.ForcedUseOriginalHost != false {
		t.Errorf("Failed to generate forced_use_original_host option")
	}

	if outPolicy.ForcedAddHeaderBeforeRouteMatcher != false {
		t.Errorf("Failed to generate forced_add_header_before_route_matcher option")
	}
}

func TestGenerateActiveRedirectPolicyWithHybridConfig(t *testing.T) {
	testInput := &networking.HTTPInternalActiveRedirect{
		MaxInternalRedirects:  3,
		RedirectResponseCodes: []uint32{500, 501},
		RedirectUrlRewriteSpecifier: &networking.HTTPInternalActiveRedirect_RedirectUrl{
			RedirectUrl: "www.tmall.com",
		},

		Policies: []*networking.HTTPInternalActiveRedirect_RedirectPolicy{
			{
				MaxInternalRedirects:  3,
				RedirectResponseCodes: []uint32{500, 501},
				RedirectUrlRewriteSpecifier: &networking.HTTPInternalActiveRedirect_RedirectPolicy_RedirectUrl{
					RedirectUrl: "www.youku.com",
				},
			},
			{
				MaxInternalRedirects:  3,
				RedirectResponseCodes: []uint32{400, 401},
				RedirectUrlRewriteSpecifier: &networking.HTTPInternalActiveRedirect_RedirectPolicy_RedirectUrl{
					RedirectUrl: "www.taobao.com",
				},
			},
		},
	}

	regexEngine := &matcher.RegexMatcher_GoogleRe2{GoogleRe2: &matcher.RegexMatcher_GoogleRE2{
		MaxProgramSize: &wrappers.UInt32Value{
			Value: uint32(10),
		},
	}}

	outPolicy := TranslateInternalActiveRedirectPolicy(testInput, regexEngine)
	if outPolicy == nil {
		t.Errorf("Failed to generate redirection policy")
	}

	if len(outPolicy.Policies) != len(testInput.Policies) {
		t.Errorf("The expectation is to generate %d redirection policies, which is actually %d", len(outPolicy.Policies), len(testInput.Policies))
	}
}

func TestGenerateActiveRedirectPolicyWithForcedUseOriginalHost(t *testing.T) {
	testInput := &networking.HTTPInternalActiveRedirect{
		MaxInternalRedirects:  3,
		RedirectResponseCodes: []uint32{500, 501},
		RedirectUrlRewriteSpecifier: &networking.HTTPInternalActiveRedirect_RedirectUrl{
			RedirectUrl: "www.taobao.com",
		},
		ForcedUseOriginalHost:             true,
		ForcedAddHeaderBeforeRouteMatcher: true,
	}

	regexEngine := &matcher.RegexMatcher_GoogleRe2{GoogleRe2: &matcher.RegexMatcher_GoogleRE2{
		MaxProgramSize: &wrappers.UInt32Value{
			Value: uint32(10),
		},
	}}

	outPolicy := TranslateInternalActiveRedirectPolicy(testInput, regexEngine)
	if outPolicy == nil {
		t.Errorf("Failed to generate redirection policy")
	}

	if outPolicy.ForcedUseOriginalHost != true {
		t.Errorf("Failed to generate forced_use_original_host option")
	}

	if outPolicy.ForcedAddHeaderBeforeRouteMatcher != true {
		t.Errorf("Failed to generate forced_add_header_before_route_matcher option")
	}
}
