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

package match

import (
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// Any doesn't filter out any echos.
var Any Matcher = func(_ echo.Instance) bool {
	return true
}

// And is an aggregate Matcher that requires all matches return true.
func And(ms ...Matcher) Matcher {
	return func(i echo.Instance) bool {
		for _, m := range ms {
			if m != nil && !m(i) {
				return false
			}
		}
		return true
	}
}

// Or is an aggregate Matcher that requires at least one matches return true.
func Or(ms ...Matcher) Matcher {
	return func(i echo.Instance) bool {
		for _, m := range ms {
			if m != nil && m(i) {
				return true
			}
		}
		return false
	}
}

// Not negates the given matcher. Example:
//
//	Not(Naked())
func Not(m Matcher) Matcher {
	return func(i echo.Instance) bool {
		return !m(i)
	}
}

// ServiceName matches instances with the given namespace and service name.
func ServiceName(n echo.NamespacedName) Matcher {
	return func(i echo.Instance) bool {
		return n == i.NamespacedName()
	}
}

// AnyServiceName matches instances if they have the same Service and Namespace as any of the provided instances.
func AnyServiceName(expected echo.NamespacedNames) Matcher {
	return func(instance echo.Instance) bool {
		serviceName := instance.NamespacedName()
		for _, expectedName := range expected {
			if serviceName == expectedName {
				return true
			}
		}
		return false
	}
}

// Namespace matches instances within the given namespace name.
func Namespace(n namespace.Instance) Matcher {
	return NamespaceName(n.Name())
}

// NamespaceName matches instances within the given namespace name.
func NamespaceName(ns string) Matcher {
	return func(i echo.Instance) bool {
		return i.Config().Namespace.Name() == ns
	}
}

// Cluster matches instances deployed on the given cluster.
func Cluster(c cluster.Cluster) Matcher {
	return func(i echo.Instance) bool {
		return c.Name() == i.Config().Cluster.Name()
	}
}

// Network matches instances deployed in the given network.
func Network(n string) Matcher {
	return func(i echo.Instance) bool {
		return i.Config().Cluster.NetworkName() == n
	}
}

// VM matches instances with DeployAsVM
var VM Matcher = func(i echo.Instance) bool {
	return i.Config().IsVM()
}

// NotVM is matches against instances that are NOT VMs.
var NotVM = Not(VM)

// External matches instances that have a custom DefaultHostHeader defined
var External Matcher = func(i echo.Instance) bool {
	return i.Config().IsExternal()
}

// NotExternal is equivalent to Not(External)
var NotExternal = Not(External)

// Naked matches instances with any subset marked with SidecarInject equal to false.
var Naked Matcher = func(i echo.Instance) bool {
	return i.Config().IsNaked()
}

// AllNaked matches instances where every subset has SidecarInject set to false.
var AllNaked Matcher = func(i echo.Instance) bool {
	return i.Config().IsAllNaked()
}

// NotNaked is equivalent to Not(Naked)
var NotNaked = Not(Naked)

// Headless matches instances that are backed by headless services.
var Headless Matcher = func(i echo.Instance) bool {
	return i.Config().Headless
}

// NotHeadless is equivalent to Not(Headless)
var NotHeadless = Not(Headless)

// ProxylessGRPC matches instances that are Pods with a SidecarInjectTemplate annotation equal to grpc.
var ProxylessGRPC Matcher = func(i echo.Instance) bool {
	return i.Config().IsProxylessGRPC()
}

// NotProxylessGRPC is equivalent to Not(ProxylessGRPC)
var NotProxylessGRPC = Not(ProxylessGRPC)

var TProxy Matcher = func(i echo.Instance) bool {
	return i.Config().IsTProxy()
}

var NotTProxy = Not(TProxy)

// RegularPod matches echos that don't meet any of the following criteria:
// - VM
// - Naked
// - Headless
// - TProxy
// - Multi-Subset
var RegularPod Matcher = func(instance echo.Instance) bool {
	return instance.Config().IsRegularPod()
}

var NotRegularPod = Not(RegularPod)

// MultiVersion matches echos that have Multi-version specific setup.
var MultiVersion Matcher = func(i echo.Instance) bool {
	if len(i.Config().Subsets) != 2 {
		return false
	}
	var matchIstio, matchLegacy bool
	for _, s := range i.Config().Subsets {
		if s.Version == "vistio" {
			matchIstio = true
		} else if s.Version == "vlegacy" && !s.Annotations.GetBool(echo.SidecarInject) {
			matchLegacy = true
		}
	}
	return matchIstio && matchLegacy
}

var NotMultiVersion = Not(MultiVersion)
