package policy

import (
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// DeprecatedAnalyzer checks for Policy or MeshPolicy resources in use when the
// PeerAuthentication resource definition exists.
type DeprecatedAnalyzer struct{}

// Compile-time check that this Analyzer correctly implements the interface
var _ analysis.Analyzer = &DeprecatedAnalyzer{}

// Metadata implements Analyzer
func (s *DeprecatedAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "policy.DeprecatedAnalyzer",
		Description: "Checks for Policy/MeshPolicy resources being used when PeerAuthentication exists",
		Inputs: collection.Names{
			collections.K8SApiextensionsK8SIoV1Beta1Customresourcedefinitions.Name(),
			collections.IstioAuthenticationV1Alpha1Meshpolicies.Name(),
			collections.IstioAuthenticationV1Alpha1Policies.Name(),
		},
	}
}

// Analyze implements Analyzer
func (s *DeprecatedAnalyzer) Analyze(c analysis.Context) {
	var hasPeerAuthenticationsCRD bool
	c.ForEach(collections.K8SApiextensionsK8SIoV1Beta1Customresourcedefinitions.Name(),
		func(r *resource.Instance) bool {
			if r.Metadata.FullName.String() == "peerauthentications.security.istio.io" {
				hasPeerAuthenticationsCRD = true
				return false
			}
			return true
		})
	if !hasPeerAuthenticationsCRD {
		return
	}

	c.ForEach(collections.IstioAuthenticationV1Alpha1Policies.Name(), func(r *resource.Instance) bool {
		c.Report(collections.IstioAuthenticationV1Alpha1Policies.Name(), msg.NewPolicyResourceIsDeprecated(r))
		return true
	})

	c.ForEach(collections.IstioAuthenticationV1Alpha1Meshpolicies.Name(), func(r *resource.Instance) bool {
		c.Report(collections.IstioAuthenticationV1Alpha1Meshpolicies.Name(), msg.NewMeshPolicyResourceIsDeprecated(r))
		return true
	})
}
