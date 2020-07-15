//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package mixer

import (
	"context"

	"github.com/hashicorp/go-multierror"

	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/pkg/attribute"

	attr "istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/server"
	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
)

const (
	telemetryService = "telemetry"
	policyService    = "policy"
	grpcPortName     = "grpc-mixer"
)

type client struct {
	// Indicates that the component is running in local mode.
	local bool

	conns      []*grpc.ClientConn
	clients    map[string]istioMixerV1.MixerClient
	forwarders []istioKube.PortForwarder

	args   *server.Args
	server *server.Server
}

// Report implements DeployedMixer.Report.
func (c *client) Report(t test.Failer, attributes map[string]interface{}) {
	t.Helper()

	req := istioMixerV1.ReportRequest{
		Attributes: []istioMixerV1.CompressedAttributes{
			getAttrBag(attributes)},
	}

	if _, err := c.clients[telemetryService].Report(context.Background(), &req); err != nil {
		t.Fatalf("Error sending report: %v", err)
	}
}

// Check implements DeployedMixer.Check.
func (c *client) Check(t test.Failer, attributes map[string]interface{}) CheckResponse {
	t.Helper()

	req := istioMixerV1.CheckRequest{
		Attributes: getAttrBag(attributes),
	}
	response, err := c.clients[policyService].Check(context.Background(), &req)

	if err != nil {
		t.Fatalf("Error sending check: %v", err)
	}

	return CheckResponse{
		Raw: response,
	}
}

// Close implements io.Closer.
func (c *client) Close() error {
	var err error
	for _, conn := range c.conns {
		err = multierror.Append(err, conn.Close()).ErrorOrNil()
	}
	c.conns = make([]*grpc.ClientConn, 0)

	for _, fw := range c.forwarders {
		fw.Close()
	}
	c.forwarders = make([]istioKube.PortForwarder, 0)

	if c.server != nil {
		err = multierror.Append(err, c.server.Close()).ErrorOrNil()
		c.server = nil
	}

	return err
}

func getAttrBag(attrs map[string]interface{}) istioMixerV1.CompressedAttributes {
	requestBag := attribute.GetMutableBag(nil)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto istioMixerV1.CompressedAttributes
	attr.ToProto(requestBag, &attrProto, nil, 0)
	return attrProto
}
