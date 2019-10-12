// Copyright 2019 Istio Authors
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
	"io/ioutil"
	"net"

	"istio.io/pkg/filewatcher"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/meshcfg"
	"istio.io/istio/galley/pkg/config/processor"
	"istio.io/istio/galley/pkg/config/source/kube"
	fs2 "istio.io/istio/galley/pkg/config/source/kube/fs"
	"istio.io/istio/galley/pkg/meshconfig"
	"istio.io/istio/galley/pkg/source/fs"
	kubeSource "istio.io/istio/galley/pkg/source/kube"
	"istio.io/istio/galley/pkg/source/kube/client"
	"istio.io/istio/galley/pkg/source/kube/schema/check"
	"istio.io/istio/pkg/mcp/monitoring"
)

// The patch table for external dependencies for code in components.
var (
	netListen                   = net.Listen
	fsNew                       = fs.New
	newKubeFromConfigFile       = client.NewKubeFromConfigFile
	newInterfaces               = kube.NewInterfacesFromConfigFile
	verifyResourceTypesPresence = check.ResourceTypesPresence
	findSupportedResources      = check.FindSupportedResourceSchemas
	newSource                   = kubeSource.New
	mcpMetricReporter           = func(prefix string) monitoring.Reporter { return monitoring.NewStatsContext(prefix) }
	newMeshConfigCache          = func(path string) (meshconfig.Cache, error) { return meshconfig.NewCacheFromFile(path) }
	newFileWatcher              = filewatcher.NewWatcher
	readFile                    = ioutil.ReadFile

	meshcfgNewFS        = func(path string) (event.Source, error) { return meshcfg.NewFS(path) }
	processorInitialize = processor.Initialize
	fsNew2              = fs2.New
)

func resetPatchTable() {
	netListen = net.Listen
	fsNew = fs.New
	newKubeFromConfigFile = client.NewKubeFromConfigFile
	newInterfaces = kube.NewInterfacesFromConfigFile
	verifyResourceTypesPresence = check.ResourceTypesPresence
	findSupportedResources = check.FindSupportedResourceSchemas
	newSource = kubeSource.New
	mcpMetricReporter = func(prefix string) monitoring.Reporter { return monitoring.NewStatsContext(prefix) }
	newMeshConfigCache = func(path string) (meshconfig.Cache, error) { return meshconfig.NewCacheFromFile(path) }
	newFileWatcher = filewatcher.NewWatcher
	readFile = ioutil.ReadFile

	meshcfgNewFS = func(path string) (event.Source, error) { return meshcfg.NewFS(path) }
	processorInitialize = processor.Initialize
	fsNew2 = fs2.New
}
