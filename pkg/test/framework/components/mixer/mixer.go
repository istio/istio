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

package mixer

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/attribute"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/internal"
	"istio.io/istio/pkg/test/kube"
)

const (
	telemetryService = "telemetry"
	policyService    = "policy"
	localServiceName = "mixer"
	grpcPortName     = "grpc-mixer"
)

var (
	_ environment.DeployedMixer = &deployedMixer{}
	_ internal.Configurable     = &deployedMixer{}
	_ io.Closer                 = &deployedMixer{}
)

type deployedMixer struct {
	// Indicates that the component is running in local mode.
	local bool

	environment environment.Implementation

	conns      []*grpc.ClientConn
	clients    map[string]istioMixerV1.MixerClient
	forwarders []kube.PortForwarder

	args    *server.Args
	server  *server.Server
	workdir string

	// AttributeManifest is injected into the configuration in the local environment. in Kubernetes, it
	// should already exist as part of Istio deployment.
	attributeManifest string
}

// Report implements DeployedMixer.Report.
func (d *deployedMixer) Report(t testing.TB, attributes map[string]interface{}) {
	t.Helper()

	expanded, err := expandAttributeTemplates(d.environment.Evaluate, attributes)
	if err != nil {
		t.Fatalf("Error expanding attribute templates: %v", err)
	}
	attributes = expanded.(map[string]interface{})

	req := istioMixerV1.ReportRequest{
		Attributes: []istioMixerV1.CompressedAttributes{
			getAttrBag(attributes)},
	}

	if _, err = d.clients[telemetryService].Report(context.Background(), &req); err != nil {
		t.Fatalf("Error sending report: %v", err)
	}
}

// Check implements DeployedMixer.Check.
func (d *deployedMixer) Check(t testing.TB, attributes map[string]interface{}) environment.CheckResponse {
	t.Helper()

	expanded, err := expandAttributeTemplates(d.environment.Evaluate, attributes)
	if err != nil {
		t.Fatalf("Error expanding attribute templates: %v", err)
	}
	attributes = expanded.(map[string]interface{})

	req := istioMixerV1.CheckRequest{
		Attributes: getAttrBag(attributes),
	}
	response, err := d.clients[policyService].Check(context.Background(), &req)

	if err != nil {
		t.Fatalf("Error sending check: %v", err)
	}

	return environment.CheckResponse{
		Raw: response,
	}
}

// ApplyConfig implements Configurable.ApplyConfig.
func (d *deployedMixer) ApplyConfig(cfg string) error {
	// This only applies when Mixer is running locally.
	if d.local {
		file := path.Join(d.workdir, "config.yaml")
		err := ioutil.WriteFile(file, []byte(cfg), os.ModePerm)

		if err == nil {
			file = path.Join(d.workdir, "attributemanifest.yaml")
			err = ioutil.WriteFile(file, []byte(d.attributeManifest), os.ModePerm)
		}

		if err == nil {
			// TODO: Implement a mechanism for reliably waiting for the configuration to disseminate in the system.
			// We can use CtrlZ to expose the config state of Mixer.
			// See https://github.com/istio/istio/issues/6169 and https://github.com/istio/istio/issues/6170.
			time.Sleep(time.Second * 3)
		}

		return err
	}

	// We shouldn't getting an ApplyConfig for the Kubernetes case.
	return fmt.Errorf("unexpected ApplyConfig call to Mixer component for Kubernetes environment: %s", cfg)
}

// Close implements io.Closer.
func (d *deployedMixer) Close() error {
	var err error
	for _, conn := range d.conns {
		err = multierror.Append(err, conn.Close()).ErrorOrNil()
	}
	d.conns = make([]*grpc.ClientConn, 0)

	for _, fw := range d.forwarders {
		fw.Close()
	}
	d.forwarders = make([]kube.PortForwarder, 0)

	if d.server != nil {
		err = multierror.Append(err, d.server.Close()).ErrorOrNil()
		d.server = nil
	}

	return err
}

func getAttrBag(attrs map[string]interface{}) istioMixerV1.CompressedAttributes {
	requestBag := attribute.GetMutableBag(nil)
	for k, v := range attrs {
		requestBag.Set(k, v)
	}

	var attrProto istioMixerV1.CompressedAttributes
	requestBag.ToProto(&attrProto, nil, 0)
	return attrProto
}

func expandAttributeTemplates(evalFn func(string) (string, error), value interface{}) (interface{}, error) {
	switch t := value.(type) {
	case string:
		return evalFn(t)

	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range t {
			// Expand key and string values.
			k, err := evalFn(k)
			if err != nil {
				return nil, err
			}
			o, err := expandAttributeTemplates(evalFn, v)
			if err != nil {
				return nil, err
			}
			result[k] = o
		}
		return result, nil

	case []interface{}:
		result := make([]interface{}, len(t))
		for i, v := range t {
			o, err := expandAttributeTemplates(evalFn, v)
			if err != nil {
				return nil, err
			}
			result[i] = o
		}
		return result, nil

	default:
		return value, nil
	}
}
