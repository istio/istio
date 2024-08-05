package route

import (
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/log"
)

const defaultRedirectResponseCode = 503

var (
	invalidRedirectResponseCodesList = []uint32{200, 301, 302, 303, 304, 307, 308}
	invalidRedirectResponseCodesMap  = map[uint32]bool{}
)

func init() {
	for _, code := range invalidRedirectResponseCodesList {
		invalidRedirectResponseCodesMap[code] = true
	}
}

func validRedirectResponseCodes(inputCodes []uint32) []uint32 {
	var validCodes []uint32

	for _, code := range inputCodes {
		if invalidRedirectResponseCodesMap[code] {
			log.Warnf("The redirect response code %d is invalid, ignore it.", code)
			continue
		}
		validCodes = append(validCodes, code)
	}

	if len(validCodes) == 0 {
		validCodes = append(validCodes, defaultRedirectResponseCode)
	}

	return validCodes
}

func TranslateInternalActiveRedirectPolicy(in *networking.HTTPInternalActiveRedirect,
	regexMatcher *matcher.RegexMatcher_GoogleRe2,
) *route.InternalActiveRedirectPolicy {
	if in == nil {
		return nil
	}

	if in.GetPolicies() != nil {
		outSidecarPolicy := &route.InternalActiveRedirectPolicy{}

		for _, inPolicy := range in.GetPolicies() {
			sidecarPolicy := &route.InternalActiveRedirectPolicy_RedirectPolicy{
				AllowCrossSchemeRedirect:          inPolicy.GetAllowCrossScheme(),
				HostRewriteLiteral:                inPolicy.GetAuthority(),
				ForcedUseOriginalHost:             inPolicy.GetForcedUseOriginalHost(),
				ForcedAddHeaderBeforeRouteMatcher: inPolicy.GetForcedAddHeaderBeforeRouteMatcher(),
			}

			// Make sure at least one redirect.
			if inPolicy.GetMaxInternalRedirects() != 0 {
				sidecarPolicy.MaxInternalRedirects = &wrappers.UInt32Value{Value: inPolicy.GetMaxInternalRedirects()}
			}

			sidecarPolicy.RedirectResponseCodes = validRedirectResponseCodes(inPolicy.GetRedirectResponseCodes())

			if in.GetRedirectUrlRewriteRegex() != nil {
				sidecarPolicy.RedirectUrlRewriteSpecifier = &route.InternalActiveRedirectPolicy_RedirectPolicy_RedirectUrlRewriteRegex{
					RedirectUrlRewriteRegex: &matcher.RegexMatchAndSubstitute{
						Pattern: &matcher.RegexMatcher{
							EngineType: regexMatcher,
							Regex:      inPolicy.GetRedirectUrlRewriteRegex().Pattern,
						},
						Substitution: inPolicy.GetRedirectUrlRewriteRegex().Substitution,
					},
				}
			} else {
				sidecarPolicy.RedirectUrlRewriteSpecifier = &route.InternalActiveRedirectPolicy_RedirectPolicy_RedirectUrl{
					RedirectUrl: inPolicy.GetRedirectUrl(),
				}
			}

			operations := TranslateHeadersOperations(inPolicy.GetHeaders())
			sidecarPolicy.RequestHeadersToAdd = operations.RequestHeadersToAdd

			outSidecarPolicy.Policies = append(outSidecarPolicy.Policies, sidecarPolicy)
		}

		return outSidecarPolicy
	}

	policy := &route.InternalActiveRedirectPolicy{
		AllowCrossSchemeRedirect:          in.GetAllowCrossScheme(),
		HostRewriteLiteral:                in.GetAuthority(),
		ForcedUseOriginalHost:             in.GetForcedUseOriginalHost(),
		ForcedAddHeaderBeforeRouteMatcher: in.GetForcedAddHeaderBeforeRouteMatcher(),
	}

	// Make sure at least one redirect.
	if in.GetMaxInternalRedirects() != 0 {
		policy.MaxInternalRedirects = &wrappers.UInt32Value{Value: in.GetMaxInternalRedirects()}
	}

	policy.RedirectResponseCodes = validRedirectResponseCodes(in.GetRedirectResponseCodes())

	if in.GetRedirectUrlRewriteRegex() != nil {
		policy.RedirectUrlRewriteSpecifier = &route.InternalActiveRedirectPolicy_RedirectUrlRewriteRegex{
			RedirectUrlRewriteRegex: &matcher.RegexMatchAndSubstitute{
				Pattern: &matcher.RegexMatcher{
					EngineType: regexMatcher,
					Regex:      in.GetRedirectUrlRewriteRegex().Pattern,
				},
				Substitution: in.GetRedirectUrlRewriteRegex().Substitution,
			},
		}
	} else {
		policy.RedirectUrlRewriteSpecifier = &route.InternalActiveRedirectPolicy_RedirectUrl{
			RedirectUrl: in.GetRedirectUrl(),
		}
	}

	operations := TranslateHeadersOperations(in.GetHeaders())
	policy.RequestHeadersToAdd = operations.RequestHeadersToAdd

	return policy
}
