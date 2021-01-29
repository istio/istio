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

package xds

import (
	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/util/gogo"
)

// PcdsGenerator generates ECDS configuration.
type PcdsGenerator struct {
	Server *DiscoveryServer
}

var _ model.XdsResourceGenerator = &PcdsGenerator{}

func pcdsNeedsPush(req *model.PushRequest) bool {
	if req == nil {
		return true
	}
	// If none set, we will always push
	if len(req.ConfigsUpdated) == 0 {
		return true
	}
	return false
}

// Generate returns ECDS resources for a given proxy.
func (e *PcdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, req *model.PushRequest) (model.Resources, error) {
	if !pcdsNeedsPush(req) {
		return nil, nil
	}

	pc := &mesh.ProxyConfig{
		// TODO update to fetch the real cert, and use the new API to store it.
		ConfigPath: `-----BEGIN CERTIFICATE-----
MIIC/DCCAeSgAwIBAgIQaw1k8vvMqtPFAhJzxammHDANBgkqhkiG9w0BAQsFADAY
MRYwFAYDVQQKEw1jbHVzdGVyLmxvY2FsMB4XDTIxMDEyNjAwMDAwNVoXDTMxMDEy
NDAwMDAwNVowGDEWMBQGA1UEChMNY2x1c3Rlci5sb2NhbDCCASIwDQYJKoZIhvcN
AQEBBQADggEPADCCAQoCggEBAKRf7qeRUmeCVKVfLpK4Doh4gWUqX4Lvu7s83Cbs
D+VD9LWSGJYzVDDPDKX/6RSLu3eOhLtmi0IxUZl6FMWOIjMgiZ9ed7FoLLV+ZAgs
CkBlZ4OzCGkKY6YtMERVUmKj2ek5wMAx/8rSfMuck51ys6oQpIQEy4cCFqfWheJM
Qjp2Tk2APzNgX8r/kPMyYTWfIti8Uj+BYEHKRcyrrIDcaD+WTidikM7qQG3VJYJX
4eb0PHcacskDAFlDT/GVxtJ+tIX4CG4Ba29YuiAiSBQkCNzU1kfbAaX6wlynjB0D
9tEoshMpeGhgGRHR38Q8QNH0pWQD5BIHC0ZPUR7UJJI4TZ0CAwEAAaNCMEAwDgYD
VR0PAQH/BAQDAgIEMA8GA1UdEwEB/wQFMAMBAf8wHQYDVR0OBBYEFN3I56u40JdT
Lmw07t9jTAuGpRpxMA0GCSqGSIb3DQEBCwUAA4IBAQCHAyTDP2iyQVrwa7Rgyj4r
kTMtk2h7akcoEnuEnw9MgJ03vB41Z224YkkgxGPG7e1QIiO9m3gCnrEd8B/JU+Ii
0sMCo4/og158SaA6pj+LgdG8DoFzH8Qxpq4boYvkIoPW7NKwzOXWvRbCWjHVi8sM
ZlU2wmHgWDfa3mq8LqU4pEZT9tGk3QtphJigMvbwSy081qxIIP6C5VN2A4T39lWe
dEZef7ApXehfCdZ8ZG5bCNZQUvMsxRPy9alw2SIipbp7/V/+AEj04GTCRRh+pvai
ITnF3YOsQ0sdbsE30t9ekoIA0U3sSDKYRTSP4c6AYrCOzyDCx6ajSPYiLNMyJOjy
-----END CERTIFICATE-----`,
	}
	return model.Resources{gogo.MessageToAny(pc)}, nil
}
