//go:build gofuzz
// +build gofuzz

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

package builder

import (
	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security/trustdomain"
)

func InternalFuzzBuildHTTP(data []byte) int {
	f := fuzz.NewConsumer(data)
	bundle := trustdomain.Bundle{}
	err := f.GenerateStruct(&bundle)

	in := &plugin.InputParams{}
	err = f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	if in.Push == nil {
		return 0
	}
	if in.Push.AuthzPolicies == nil {
		return 0
	}
	if in.Node == nil {
		return 0
	}
	if in.Node.Metadata == nil {
		return 0
	}

	policies := in.Push.AuthzPolicies.ListAuthorizationPolicies(in.Node.ConfigNamespace, in.Node.Metadata.Labels)
	option := Option{}
	err = f.GenerateStruct(&option)
	if err != nil {
		return 0
	}
	g := New(bundle, in.Push, policies, option)
	if g == nil {
		return 0
	}
	g.BuildHTTP()
	return 1
}

func InternalFuzzBuildTCP(data []byte) int {
	f := fuzz.NewConsumer(data)
	bundle := trustdomain.Bundle{}
	err := f.GenerateStruct(&bundle)

	in := &plugin.InputParams{}
	err = f.GenerateStruct(in)
	if err != nil {
		return 0
	}
	if in.Push == nil {
		return 0
	}
	if in.Push.AuthzPolicies == nil {
		return 0
	}
	if in.Node == nil {
		return 0
	}
	if in.Node.Metadata == nil {
		return 0
	}

	policies := in.Push.AuthzPolicies.ListAuthorizationPolicies(in.Node.ConfigNamespace, in.Node.Metadata.Labels)
	option := Option{}
	err = f.GenerateStruct(&option)
	if err != nil {
		return 0
	}
	g := New(bundle, in.Push, policies, option)
	if g == nil {
		return 0
	}
	g.BuildTCP()
	return 1
}
