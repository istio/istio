// Copyright 2018 Istio Authors
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

package server

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/server/settings"
)

func TestServer(t *testing.T) {
	g := NewGomegaWithT(t)

	rootDir, err := ioutil.TempDir(os.TempDir(), t.Name())
	g.Expect(err).To(BeNil())

	meshDir := path.Join(rootDir, "meshcfg")
	meshFile := path.Join(meshDir, "mesh.yaml")
	configDir := path.Join(rootDir, "cfg")
	accessListDir := path.Join(rootDir, "access")
	accessListFile := path.Join(accessListDir, "access.yaml")

	err = os.Mkdir(meshDir, os.ModePerm)
	g.Expect(err).To(BeNil())

	err = os.Mkdir(configDir, os.ModePerm)
	g.Expect(err).To(BeNil())

	err = os.Mkdir(accessListDir, os.ModePerm)
	g.Expect(err).To(BeNil())

	err = ioutil.WriteFile(meshFile, nil, os.ModePerm)
	g.Expect(err).To(BeNil())

	a := settings.DefaultArgs()
	// If the default port is used it may run into conflicts during testing
	a.IntrospectionOptions.Port = 0
	a.MeshConfigFile = meshFile
	a.ConfigPath = configDir
	a.AccessListFile = accessListFile
	a.Insecure = true
	a.EnableValidationController = false
	a.EnableValidationServer = false
	a.EnableProfiling = true

	s := New(a)

	g.Expect(s.Address()).To(BeNil())

	err = s.Start()
	g.Expect(err).To(BeNil())

	g.Expect(s.Address()).NotTo(BeNil())
	s.Stop()
	g.Expect(s.Address()).To(BeNil())

	s.Stop() // Do not panic
}
