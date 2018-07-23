//  Copyright 2018 Istio Authors
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

package cluster

import (
	"context"
	"io"
	"testing"

	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/kube"
)

type deployedMixer struct {
	conn   *grpc.ClientConn
	client istio_mixer_v1.MixerClient

	forwarder *kube.PortForwarder

	args *server.Args
}

var _ environment.DeployedMixer = &deployedMixer{}
var _ io.Closer = &deployedMixer{}

func newMixer(kubeconfigPath string, accessor *kube.Accessor) (*deployedMixer, error) {
	pod, err := accessor.WaitForPodBySelectors("istio-system", "istio=mixer", "istio-mixer-type=telemetry")
	if err != nil {
		return nil, err
	}

	// TODO: Add support to connect to the Mixer istio-policy as well.
	// See https://github.com/istio/istio/issues/6174

	// TODO: Right now, simply connect to the telemetry backend at port 9092. We can expand this to connect
	// to policy backend and dynamically figure out ports later.
	// See https://github.com/istio/istio/issues/6175
	forwarder := kube.NewPortForwarder(kubeconfigPath, pod.Namespace, pod.Name, 9092)

	if err = forwarder.Start(); err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(forwarder.Address(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	client := istio_mixer_v1.NewMixerClient(conn)

	return &deployedMixer{
		client:    client,
		forwarder: forwarder,
		// Use the DefaultArgs to get config identity attribute
		args: server.DefaultArgs(),
	}, nil
}

// Report implementation
func (d *deployedMixer) Report(t testing.TB, attributes map[string]interface{}) {
	t.Helper()
	scope.Debugf("Reporting attributes:\n%v\n", attributes)

	req := istio_mixer_v1.ReportRequest{
		Attributes: []istio_mixer_v1.CompressedAttributes{
			getAttrBag(attributes,
				d.args.ConfigIdentityAttribute,
				d.args.ConfigIdentityAttributeDomain)},
	}
	_, err := d.client.Report(context.Background(), &req)

	if err != nil {
		t.Fatalf("Error sending report: %v", err)
	}
}

// Close implementation.
func (d *deployedMixer) Close() (err error) {
	if d.conn != nil {
		err = d.conn.Close()
	}

	if d.forwarder != nil {
		d.forwarder.Close()
	}

	return
}

func getAttrBag(attrs map[string]interface{}, identityAttr, identityAttrDomain string) istio_mixer_v1.CompressedAttributes {
	requestBag := attribute.GetMutableBag(nil)
	requestBag.Set(identityAttr, identityAttrDomain)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto istio_mixer_v1.CompressedAttributes
	requestBag.ToProto(&attrProto, nil, 0)
	return attrProto
}
