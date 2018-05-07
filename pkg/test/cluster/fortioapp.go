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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/tests/util"
)

type fortioapp struct {
	name          string
	namespace     string
	serverAddress string
	labels        []string
}

// NewFortioApp creates a new fortioapp object from the given service config
func NewFortioApp(meta model.ConfigMeta, serverAddress string, labels []string) test.DeployedFortioApp {
	a := &fortioapp{}
	a.name = meta.Name
	a.namespace = meta.Namespace
	a.serverAddress = serverAddress

	for _, label := range labels {
		a.labels = append(a.labels, label)
	}

	return a
}

// CallFortio implements the test.DeployedApp interface
func (f *fortioapp) CallFortio(arg string, path string) (test.AppCallFortioResponse, error) {
	pods, err := getPods()
	if err != nil {
		return nil, err
	}
	if len(pods) != 1 {
		return nil, errors.New("Expected only one pod instance, but %d.", len(pods))
	}

	pod := pods[0]

	response, err := util.Shell("kubectl exec -n %s %s -c %s -- /usr/local/bin/fortio %s %s/%s", f.namespace, pod, f.name, f.serverAddress, arg, path)
	if err != nil {
		return nil, err
	}

	out := test.AppCallFortioResponse{
		raw: response,
	}
	return out, nil
}

func (f *fortioapp) getPods() ([]string, error) {
	out, err := util.Shell("kubectl get pods -n %s -l %s -o jsonpath={.items[*].metadata.name}", f.namespace, f.labelsToString())
	if err != nil {
		return nil, err
	}
	return strings.Split(out, " "), nil
}

func (f *fortioapp) labelsToString() {
	str := ""
	for _, label := range f.labels {
		str = str + label + ","
	}
	str = strings.TrimSuffix(str, ",")
	return str
}
