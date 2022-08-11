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

	"istio.io/istio/operator/cmd/mesh"
	"istio.io/istio/operator/pkg/util/clog"
	testenv "istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/pkg/log"
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
		InFilenames:    args.Files,
		KubeConfigPath: kubeConfigFile,
		Set:            args.Set,
		ManifestsPath:  filepath.Join(testenv.IstioSrc, "manifests"),
		Revision:       args.Revision,
	}

	// Generate the manifest YAML, so that we can uninstall it in Close.
	var stdOut, stdErr bytes.Buffer
	if err := mesh.ManifestGenerate(&mesh.RootArgs{}, &mesh.ManifestGenerateArgs{
		InFilenames:   iArgs.InFilenames,
		Set:           iArgs.Set,
		Force:         iArgs.Force,
		ManifestsPath: iArgs.ManifestsPath,
		Revision:      iArgs.Revision,
	}, cmdLogOptions(), cmdLogger(&stdOut, &stdErr)); err != nil {
		return err
	}
	yaml := stdOut.String()

	// Store the generated manifest.
	i.mu.Lock()
	i.manifests[c.Name()] = append(i.manifests[c.Name()], yaml)
	i.mu.Unlock()

	// Actually run the install command
	iArgs.SkipConfirmation = true

	componentName := args.ComponentName
	if len(componentName) == 0 {
		componentName = "Istio components"
	}

	scopes.Framework.Infof("Installing %s on cluster %s: %s", componentName, c.Name(), iArgs)
	stdOut.Reset()
	stdErr.Reset()
	if err := mesh.Install(&mesh.RootArgs{}, iArgs, cmdLogOptions(), &stdOut,
		cmdLogger(&stdOut, &stdErr),
		mesh.NewPrinterForWriter(&stdOut)); err != nil {
		return fmt.Errorf("failed installing %s on cluster %s: %v. Details: %s", componentName, c.Name(), err, &stdErr)
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
	return nil
}

func (i *installer) Dump(resource.Context) {
	manifestsDir := path.Join(i.workDir, "manifests")
	if err := os.Mkdir(manifestsDir, 0o700); err != nil {
		scopes.Framework.Errorf("Unable to create directory for dumping install manifests: %v", err)
	}
	for clusterName, manifests := range i.manifests {
		clusterDir := path.Join(manifestsDir, clusterName)
		if err := os.Mkdir(manifestsDir, 0o700); err != nil {
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

func cmdLogOptions() *log.Options {
	o := log.DefaultOptions()

	// These scopes are, at the default "INFO" level, too chatty for command line use
	o.SetOutputLevel("validation", log.ErrorLevel)
	o.SetOutputLevel("processing", log.ErrorLevel)
	o.SetOutputLevel("analysis", log.WarnLevel)
	o.SetOutputLevel("installer", log.WarnLevel)
	o.SetOutputLevel("translator", log.WarnLevel)
	o.SetOutputLevel("adsc", log.WarnLevel)
	o.SetOutputLevel("default", log.WarnLevel)
	o.SetOutputLevel("klog", log.WarnLevel)
	o.SetOutputLevel("kube", log.ErrorLevel)

	return o
}

func cmdLogger(stdOut, stdErr io.Writer) clog.Logger {
	return clog.NewConsoleLogger(stdOut, stdErr, scopes.Framework)
}
