package builder

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/fuzz"
)

func FuzzBuildHTTP(f *testing.F) {
	fuzz.BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		bundle := fuzz.Struct[trustdomain.Bundle](fg)
		push := fuzz.Struct[*model.PushContext](fg, validatePush)
		node := fuzz.Struct[*model.Proxy](fg)
		policies := push.AuthzPolicies.ListAuthorizationPolicies(node.ConfigNamespace, node.Metadata.Labels)
		option := fuzz.Struct[Option](fg)
		New(bundle, push, policies, option).BuildHTTP()
	})
}

func FuzzBuildTCP(f *testing.F) {
	fuzz.BaseCases(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		fg := fuzz.New(t, data)
		bundle := fuzz.Struct[trustdomain.Bundle](fg)
		push := fuzz.Struct[*model.PushContext](fg, validatePush)
		node := fuzz.Struct[*model.Proxy](fg)
		policies := push.AuthzPolicies.ListAuthorizationPolicies(node.ConfigNamespace, node.Metadata.Labels)
		option := fuzz.Struct[Option](fg)
		New(bundle, push, policies, option).BuildTCP()
	})
}

func validatePush(in *model.PushContext) bool {
	if in == nil {
		return false
	}
	if in.AuthzPolicies == nil {
		return false
	}
	return true
}
