package auth

import (
	"istio.io/api/authentication/v1alpha1"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"

	"istio.io/istio/galley/pkg/config/analysis/msg"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/auth/mtls"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// MTLSAnalyzer checks for misconfigurations of MTLS policy. More specifically,
// it detects situations where a DestinationRule's MTLS usage is in conflict
// with mTLS specified by a policy.
//
// The most common situations that this detects are: 1. A MeshPolicy exists that
// requires mTLS, but no global destination rule
//    says to use mTLS.
// 2. mTLS is used throughout the mesh, but a DestinationRule is added that
//    doesn't specify mTLS (usually because it was forgotten).
//
// The analyzer tries to act more generally by imagining service-to-service
// traffic and detecting whether or not there's a conflict with regards to mTLS
// policy. This means it will also detect explicit misconfigurations as well.
//
// Note this is very similar to `istioctl authn tls-check`; however this
// inspection is all done via analyzing configuration rather than requiring a
// connection to pilot.
type MTLSAnalyzer struct{}

// Compile-time check that this Analyzer correctly implements the interface
var _ analysis.Analyzer = &MTLSAnalyzer{}

// Metadata implements Analyzer
func (s *MTLSAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "auth.MTLSAnalyzer",

		// Each analyzer should register the collections that it needs to use as input.
		Inputs: collection.Names{
			metadata.K8SCoreV1Namespaces,
			metadata.IstioAuthenticationV1Alpha1Meshpolicies,
			metadata.IstioAuthenticationV1Alpha1Policies,
			metadata.IstioNetworkingV1Alpha3Destinationrules,
		},
	}
}

// Analyze implements Analyzer
func (s *MTLSAnalyzer) Analyze(c analysis.Context) {
	// TODO Reuse pilot logic as a library rather than reproducing its logic
	// here.

	// While we visit every item, collect the set of namespaces that exist. Note
	// that we will collect the namespace name for all resource types - this
	// ensures our analyzer still behaves correctly even if namespaces are
	// implicitly defined.
	namespaces := make(map[string]struct{})

	c.ForEach(metadata.K8SCoreV1Namespaces, func(r *resource.Entry) bool {
		_, name := r.Metadata.Name.InterpretAsNamespaceAndName()
		namespaces[name] = struct{}{}
		return true
	})

	pc := mtls.NewPolicyChecker()
	c.ForEach(metadata.IstioAuthenticationV1Alpha1Meshpolicies, func(r *resource.Entry) bool {
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		namespaces[ns] = struct{}{}

		pc.AddMeshPolicy(r, r.Item.(*v1alpha1.Policy))
		return true
	})

	c.ForEach(metadata.IstioAuthenticationV1Alpha1Policies, func(r *resource.Entry) bool {
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		namespaces[ns] = struct{}{}

		err := pc.AddPolicy(r, r.Item.(*v1alpha1.Policy))
		if err != nil {
			c.Report(metadata.IstioAuthenticationV1Alpha1Meshpolicies, msg.NewInternalError(r, err.Error()))
			return false
		}
		return true
	})

	drc := mtls.NewDestinationRuleChecker()
	c.ForEach(metadata.IstioNetworkingV1Alpha3Destinationrules, func(r *resource.Entry) bool {
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		namespaces[ns] = struct{}{}

		drc.AddDestinationRule(r, r.Item.(*v1alpha3.DestinationRule))
		return true
	})

	// Here we explicitly handle the common case where a user specifies a
	// MeshPolicy with no global MTLS rule
	mpr := pc.MeshPolicy()
	globalMtls, globalDR := drc.DoesNamespaceUseMTLSToService("istio-system", "istio-system", mtls.NewTargetService("*.local"))
	if mpr.MTLSMode == mtls.ModeStrict && !globalMtls {
		// We may or may not have a matching DR. If we don't, use
		// the special string (none)
		globalDRName := "(none)"
		if globalDR != nil {
			globalDRName = globalDR.Origin.FriendlyName()
		}
		c.Report(metadata.IstioAuthenticationV1Alpha1Meshpolicies, msg.NewMTLSPolicyConflict(mpr.Resource, "*.local", "istio-system", globalDRName, globalMtls, mpr.Resource.Origin.FriendlyName(), mpr.MTLSMode.String()))
	}

	// Also handle the less-common case where a global DR exists that specifies
	// mtls, but MTLS is off
	if mpr.MTLSMode == mtls.ModeOff && globalMtls {
		// We may or may not have a matching policy. If we don't, use
		// the special string (none)
		globalPolicyName := "(none)"
		if mpr.Resource != nil {
			globalPolicyName = mpr.Resource.Origin.FriendlyName()
		}
		c.Report(metadata.IstioNetworkingV1Alpha3Destinationrules, msg.NewMTLSPolicyConflict(globalDR, "*.local", "istio-system", globalDR.Origin.FriendlyName(), globalMtls, globalPolicyName, mpr.MTLSMode.String()))
	}

	// We've consumed all the relevant resources. Now collect every service as a
	// fqdn (generated from all destination rules and policies).
	targetServices := make(map[mtls.TargetService]struct{})
	for _, ts := range pc.TargetServices() {
		// Filter out target services that aren't namespace specific (those
		// special cases were handled above).
		if ns, _ := util.GetNamespaceAndNameFromFQDN(ts.FQDN()); ns == "" {
			continue
		}
		targetServices[ts] = struct{}{}
	}
	for _, ts := range drc.TargetServices() {
		// Filter out target services that aren't namespace specific (those
		// special cases were handled above).
		if ns, _ := util.GetNamespaceAndNameFromFQDN(ts.FQDN()); ns == "" {
			continue
		}
		targetServices[ts] = struct{}{}
	}

	// Iterate over all fqdns and namespaces, and check that the mtls mode
	// specified by the destination rule and the policy are not in conflict.
	for ts := range targetServices {
		tsPolicy, err := pc.IsServiceMTLSEnforced(ts)
		if err != nil {
			c.Report(metadata.IstioAuthenticationV1Alpha1Policies, msg.NewInternalError(nil, err.Error()))
			return
		}

		// Extract out the namespace for the target service (which should always
		// exist as we filtered out non-namespace services earlier).
		tsNamespace, _ := util.GetNamespaceAndNameFromFQDN(ts.FQDN())

		for ns := range namespaces {
			mtlsUsed, matchingDR := drc.DoesNamespaceUseMTLSToService(ns, tsNamespace, ts)
			if (tsPolicy.MTLSMode == mtls.ModeStrict && !mtlsUsed) ||
				(tsPolicy.MTLSMode == mtls.ModeOff && mtlsUsed) {
				if tsPolicy.Resource != nil {
					// We may or may not have a matching DR. If we don't, use
					// the special string (none)
					matchingDRName := "(none)"
					if matchingDR != nil {
						matchingDRName = matchingDR.Origin.FriendlyName()
					}
					c.Report(metadata.IstioAuthenticationV1Alpha1Policies, msg.NewMTLSPolicyConflict(tsPolicy.Resource, ts.FQDN(), ns, matchingDRName, mtlsUsed, tsPolicy.Resource.Origin.FriendlyName(), tsPolicy.MTLSMode.String()))
				}
				if matchingDR != nil {
					// We may or may not have a matching policy. If we don't, use
					// the special string (none)
					policyName := "(none)"
					if tsPolicy.Resource != nil {
						policyName = tsPolicy.Resource.Origin.FriendlyName()
					}
					c.Report(metadata.IstioNetworkingV1Alpha3Destinationrules, msg.NewMTLSPolicyConflict(matchingDR, ts.FQDN(), ns, matchingDR.Origin.FriendlyName(), mtlsUsed, policyName, tsPolicy.MTLSMode.String()))
				}
			}
		}
	}
}
