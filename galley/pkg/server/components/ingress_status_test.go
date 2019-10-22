// Copyright 2017 Istio Authors
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

package components

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"

	meshconfigapi "istio.io/api/mesh/v1alpha1"
	meshconfig "istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/server/settings"
	"istio.io/istio/galley/pkg/testing/mock"
)

func TestStatus(t *testing.T) {
	g := NewWithT(t)
	resetPatchTable()
	defer resetPatchTable()

	mk := mock.NewKube()
	cl, err := mk.KubeClient()
	g.Expect(err).ToNot(HaveOccurred())
	mk.AddResponse(cl, nil)

	args := defaultSyncArgs(*g)

	s := NewIngressStatusSyncer(cl, args)
	g.Expect(s).ToNot(BeNil())

	err = s.Start()
	g.Expect(err).ToNot(HaveOccurred())

	s.Stop()
}

func TestNewIngressStatusSyncerWithErrors(t *testing.T) {
	g := NewWithT(t)
	resetPatchTable()
	defer resetPatchTable()

	mk := mock.NewKube()
	client, err := mk.KubeClient()
	g.Expect(err).ToNot(HaveOccurred())
	mk.AddResponse(client, nil)

	cases := []struct {
		description     string
		clientExists    bool
		meshConfigCache func(string) (meshconfig.Cache, error)
	}{
		{
			description:  "no kube client",
			clientExists: false,
		},
		{
			description: "mesh config cache error",
			meshConfigCache: func(string) (meshconfig.Cache, error) {
				return nil, fmt.Errorf("error getting mesh config")
			},
		},
	}

	for _, c := range cases {
		args := defaultSyncArgs(*g)

		if c.clientExists == false {
			client = nil
		}

		if c.meshConfigCache != nil {
			newMeshConfigCache = c.meshConfigCache
		}

		s := NewIngressStatusSyncer(client, args)
		g.Expect(s).To(BeNil())
	}

}

func defaultSyncArgs(g GomegaWithT) *settings.Args {
	args := settings.DefaultArgs()
	args.EnableServiceDiscovery = true

	tmpDir, _ := ioutil.TempDir(os.TempDir(), "status-syncer")
	meshCfgFile := path.Join(tmpDir, "meshcfg.yaml")
	_, err := os.Create(meshCfgFile)
	g.Expect(err).ToNot(HaveOccurred())

	args.MeshConfigFile = meshCfgFile

	return args
}

func TestShouldUpdateStatus(t *testing.T) {
	g := NewWithT(t)

	cases := []struct {
		mode                   meshconfigapi.MeshConfig_IngressControllerMode
		annotation             string
		meshConfigIngressClass string
		update                 bool
	}{
		{
			mode:                   meshconfigapi.MeshConfig_DEFAULT,
			annotation:             "",
			meshConfigIngressClass: "some-class",
			update:                 true,
		},
		{
			mode:                   meshconfigapi.MeshConfig_DEFAULT,
			annotation:             "some-annotation",
			meshConfigIngressClass: "some-class",
			update:                 false,
		},
		{
			mode:                   meshconfigapi.MeshConfig_STRICT,
			annotation:             "some-specific-class",
			meshConfigIngressClass: "some-specific-class",
			update:                 true,
		},
		{
			mode:                   meshconfigapi.MeshConfig_STRICT,
			annotation:             "some-annotation",
			meshConfigIngressClass: "some-class",
			update:                 false,
		},
	}

	for _, c := range cases {
		result := shouldUpdateStatus(c.mode, c.annotation, c.meshConfigIngressClass)
		g.Expect(result).To(Equal(c.update))
	}
}
