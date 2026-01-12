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

package istio

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"k8s.io/client-go/rest"

	"istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/operator/pkg/util/clog"
	"istio.io/istio/pkg/kube"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var _ resource.Dumper = &installer{}

type installArgs struct {
	ComponentName string
	Revision      string
	Files         []string
	Set           []string
}

func (args *installArgs) AppendSet(key, value string) {
	args.Set = append(args.Set, fmt.Sprintf("%s=%s", key, value))
}

type installer struct {
	ctx       resource.Context
	workDir   string
	manifests map[string][]string
	mu        sync.Mutex
}

func newInstaller(ctx resource.Context, workDir string) *installer {
	return &installer{
		ctx:       ctx,
		workDir:   workDir,
		manifests: make(map[string][]string),
	}
}

func (i *installer) Install(c cluster.Cluster, args installArgs) error {
	kubeConfigFile, err := kubeConfigFileForCluster(c)
	if err != nil {
		return err
	}

	iArgs := &mesh.InstallArgs{
		InFilenames:   args.Files,
		Set:           args.Set,
		ManifestsPath: filepath.Join(testenv.IstioSrc, "manifests"),
		Revision:      args.Revision,
	}
	if i.ctx.Settings().Ambient {
		iArgs.InFilenames = append(iArgs.InFilenames, filepath.Join(testenv.IstioSrc, IntegrationTestAmbientDefaultsIOP))
	}
	if i.ctx.Settings().PeerMetadataDiscovery && len(args.ComponentName) == 0 {
		iArgs.InFilenames = append(iArgs.InFilenames, filepath.Join(testenv.IstioSrc, IntegrationTestPeerMetadataDiscoveryDefaultsIOP))
	}

	rc, err := kube.DefaultRestConfig(kubeConfigFile, "", func(config *rest.Config) {
		config.QPS = 50
		config.Burst = 100
	})
	if err != nil {
		return err
	}
	kubeClient, err := kube.NewCLIClient(kube.NewClientConfigForRestConfig(rc))
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %v", err)
	}

	// Generate the manifest YAML, so that we can uninstall it in Close.
	var stdOut, stdErr bytes.Buffer
	if err := mesh.ManifestGenerate(kubeClient, &mesh.ManifestGenerateArgs{
		InFilenames:   iArgs.InFilenames,
		Set:           iArgs.Set,
		Force:         iArgs.Force,
		ManifestsPath: iArgs.ManifestsPath,
		Revision:      iArgs.Revision,
	}, cmdLogger(&stdOut, &stdErr)); err != nil {
		return err
	}
	yaml := stdOut.String()

	// Store the generated manifest.
	i.mu.Lock()
	i.manifests[c.Name()] = append(i.manifests[c.Name()], yaml)
	i.mu.Unlock()

	// Actually run the install command
	iArgs.SkipConfirmation = true
	iArgs.ReadinessTimeout = time.Minute * 5

	componentName := args.ComponentName
	if len(componentName) == 0 {
		componentName = "Istio components"
	}

	scopes.Framework.Infof("Installing %s on cluster %s: %s", componentName, c.Name(), iArgs)
	stdOut.Reset()
	stdErr.Reset()
	if err := mesh.Install(kubeClient, &mesh.RootArgs{}, iArgs, &stdOut,
		cmdLogger(&stdOut, &stdErr),
		mesh.NewPrinterForWriter(&stdOut)); err != nil {
		return fmt.Errorf("failed installing %s on cluster %s: %v. Details: %s", componentName, c.Name(), err, &stdErr)
	}
	if componentName == "eastwestgateway" {
		scopes.Framework.Infof("Installed %s on cluster %s: %s, yaml: %s", componentName, c.Name(), iArgs, yaml)
	}
	return nil
}

func (i *installer) Close(c cluster.Cluster) error {
	i.mu.Lock()
	manifests := i.manifests[c.Name()]
	delete(i.manifests, c.Name())
	i.mu.Unlock()

	if len(manifests) > 0 {
		return i.ctx.ConfigKube(c).YAML("", removeCRDsSlice(manifests)).Delete()
	}
	scopes.Framework.Debugf("Deleting yaml on cluster %s: %+v", c.Name(), manifests)
	return nil
}

func (i *installer) Dump(resource.Context) {
	manifestsDir := path.Join(i.workDir, "manifests")
	if err := os.Mkdir(manifestsDir, 0o700); err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping install manifests: %v", err)
	}
	for clusterName, manifests := range i.manifests {
		clusterDir := path.Join(manifestsDir, clusterName)
		if err := os.Mkdir(clusterDir, 0o700); err != nil {
			scopes.Framework.Errorf("Unable to create directory for dumping %s install manifests: %v", clusterName, err)
		}
		for i, manifest := range manifests {
			err := os.WriteFile(path.Join(clusterDir, "manifest-"+strconv.Itoa(i)+".yaml"), []byte(manifest), 0o644)
			if err != nil {
				scopes.Framework.Errorf("Failed writing manifest %d/%d in %s: %v", i, len(manifests)-1, clusterName, err)
			}
		}
	}
}

func cmdLogger(stdOut, stdErr io.Writer) clog.Logger {
	return clog.NewConsoleLogger(stdOut, stdErr, scopes.Framework)
}
