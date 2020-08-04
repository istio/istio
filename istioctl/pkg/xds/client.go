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

// xds uses ADSC to call XDS

import (
	"fmt"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/adsc"
)

// GetXdsResponse opens a gRPC connection to opts.xds and waits for a single response
func GetXdsResponse(dr *xdsapi.DiscoveryRequest, opts *clioptions.CentralControlPlaneOptions) (*xdsapi.DiscoveryResponse, error) {
	adscConn, err := adsc.Dial(opts.Xds, opts.CertDir, &adsc.Config{
		Meta: model.NodeMetadata{
			Generator: "event",
		}.ToStruct(),
		InsecureSkipVerify: opts.InsecureSkipVerify,
		XDSSAN:             opts.XDSSAN,
	})
	if err != nil {
		return nil, fmt.Errorf("could not dial: %w", err)
	}

	err = adscConn.Send(dr)
	if err != nil {
		return nil, err
	}

	response, err := adscConn.WaitVersion(opts.Timeout, dr.TypeUrl, "")
	return response, err
}
