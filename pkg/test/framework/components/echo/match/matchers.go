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
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
)

// Any doesn't filter out any echos.
var Any Matcher = func(_ echo.Instance) bool {
	return true
}

// And is an aggregate Matcher that requires all matches return true.
func And(ms ...Matcher) Matcher {
	return func(i echo.Instance) bool {
		for _, m := range ms {
			if !m(i) {
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
			if m(i) {
				return true
			}
		}
		return false
	}
}

// Not negates the given matcher. Example:
//     Not(IsNaked())
func Not(m Matcher) Matcher {
	return func(i echo.Instance) bool {
		return !m(i)
	}
}

// ServicePrefix matches instances whose service name starts with the given prefix.
func ServicePrefix(prefix string) Matcher {
	return func(i echo.Instance) bool {
		return strings.HasPrefix(i.Config().Service, prefix)
	}
}

// SameService matches instances with the same namespace and service name as the provided resource.
func SameService(c echo.Configurable) Matcher {
	return NamespacedName(c.NamespacedName())
}

// NamespacedName matches instances with the given namespace and service name.
func NamespacedName(n model.NamespacedName) Matcher {
	return func(i echo.Instance) bool {
		return n == i.Config().NamespacedName()
	}
}

// Service matches instances with have the given service name.
func Service(value string) Matcher {
	return func(i echo.Instance) bool {
		return value == i.Config().Service
	}
}

// FQDN matches instances with have the given fully qualified domain name.
func FQDN(value string) Matcher {
	return func(i echo.Instance) bool {
		return value == i.Config().ClusterLocalFQDN()
	}
}

// SameDeployment matches instnaces with the same FQDN and assumes they're part of the same Service and Namespace.
func SameDeployment(match echo.Instance) Matcher {
	return func(instance echo.Instance) bool {
		return match.Config().ClusterLocalFQDN() == instance.Config().ClusterLocalFQDN()
	}
}

// Namespace matches instances within the given namespace name.
func Namespace(namespace string) Matcher {
	return func(i echo.Instance) bool {
		return i.Config().Namespace.Name() == namespace
	}
}

// InCluster matches instances deployed on the given cluster.
func InCluster(c cluster.Cluster) Matcher {
	return func(i echo.Instance) bool {
		return c.Name() == i.Config().Cluster.Name()
	}
}

// InNetwork matches instances deployed in the given network.
func InNetwork(n string) Matcher {
	return func(i echo.Instance) bool {
		return i.Config().Cluster.NetworkName() == n
	}
}

// IsVM matches instances with DeployAsVM
var IsVM Matcher = func(i echo.Instance) bool {
	return i.Config().IsVM()
}

// IsNotVM is matches against instances that are NOT VMs.
var IsNotVM = Not(IsVM)

// IsExternal matches instances that have a custom DefaultHostHeader defined
var IsExternal Matcher = func(i echo.Instance) bool {
	return i.Config().IsExternal()
}

// IsNotExternal is equivalent to Not(IsExternal)
var IsNotExternal = Not(IsExternal)

// IsNaked matches instances that are Pods with a SidecarInject annotation equal to false.
var IsNaked Matcher = func(i echo.Instance) bool {
	return i.Config().IsNaked()
}

// IsNotNaked is equivalent to Not(IsNaked)
var IsNotNaked = Not(IsNaked)

// IsHeadless matches instances that are backed by headless services.
var IsHeadless Matcher = func(i echo.Instance) bool {
	return i.Config().Headless
}

// IsNotHeadless is equivalent to Not(IsHeadless)
var IsNotHeadless = Not(IsHeadless)

// IsProxylessGRPC matches instances that are Pods with a SidecarInjectTemplate annotation equal to grpc.
var IsProxylessGRPC Matcher = func(i echo.Instance) bool {
	return i.Config().IsProxylessGRPC()
}

// IsNotProxylessGRPC is equivalent to Not(IsProxylessGRPC)
var IsNotProxylessGRPC = Not(IsProxylessGRPC)

var IsTProxy Matcher = func(i echo.Instance) bool {
	return i.Config().IsTProxy()
}

var IsNotTProxy = Not(IsTProxy)
