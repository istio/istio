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

package images

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/docker"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/crosscompile"
)

const (
	noSidecarDockerFile = "pkg/test/echo/docker/Dockerfile.app"
	sidecarDockerFile   = "pkg/test/echo/docker/Dockerfile.app_sidecar"
	sidecarStartScript  = "pkg/test/echo/docker/echo-start.sh"
	certsDir            = "tests/testdata/certs"
	certFile            = certsDir + "/cert.crt"
	keyFile             = certsDir + "/cert.key"
	echoClientDir       = "pkg/test/echo/cmd/client"
	echoServerDir       = "pkg/test/echo/cmd/server"
	pilotAgentDir       = "pilot/cmd/pilot-agent"
	nodeAgentDir        = "security/cmd/node_agent"
	packagingDir        = "tools/packaging"
	packagingCommonDir  = packagingDir + "/common"
	postinstScript      = packagingDir + "/deb/postinst.sh"
	noSidecarImageName  = "app"
	sidecarImageName    = "app_sidecar"
)

var (
	sidecarBuilder   *docker.ImageBuilder
	noSidecarBuilder *docker.ImageBuilder
	initMutex        sync.Mutex
	prebuilt         *Instance
)

func init() {
	if env.TAG.Value() != "" {
		hubPrefix := ""
		if env.HUB.Value() != "" {
			hubPrefix = env.HUB.Value() + "/"
		}

		prebuilt = &Instance{
			NoSidecar: docker.Image(fmt.Sprintf("%s%s:%s", hubPrefix, noSidecarImageName, env.TAG.Value())),
			Sidecar:   docker.Image(fmt.Sprintf("%s%s:%s", hubPrefix, sidecarImageName, env.TAG.Value())),
		}

		scopes.CI.Infof("[Echo Docker]: Using pre-built Docker images: %+v", *prebuilt)
	}
}

// Instance for the Echo application
type Instance struct {
	// NoSidecar is the Docker image for the plain Echo application without a sidecar.
	NoSidecar docker.Image

	// Sidecar is the Docker image for the Echo application bundled with the sidecar (pilot-agent, envoy, ip-tables, etc.)
	Sidecar docker.Image
}

// Get the images for the Echo application.
func Get(e *native.Environment) (Instance, error) {
	if prebuilt != nil {
		return *prebuilt, nil
	}

	// Lazy-initialize the image builders.
	if err := initBuilders(); err != nil {
		return Instance{}, err
	}

	imageRegistry, err := e.ImageRegistry()
	if err != nil {
		return Instance{}, err
	}

	scopes.CI.Infof("[Echo Docker]: Building images...")
	tStart := time.Now()

	out := Instance{}

	// Build the Docker images in parallel.
	wg := &sync.WaitGroup{}
	mux := sync.Mutex{}
	var aggregateError error
	wg.Add(1)
	go func() {
		defer wg.Done()

		scopes.CI.Infof("[Echo Docker]: Building no-sidecar image")
		tStart := time.Now()

		image, err := imageRegistry.GetOrCreate(noSidecarImageName, noSidecarBuilder.Build)

		mux.Lock()
		defer mux.Unlock()
		if err != nil {
			aggregateError = multierror.Append(aggregateError, err)
			return
		}

		out.NoSidecar = image
		scopes.CI.Infof("[Echo Docker]: Completed building no-sidecar image (elapsed time: %fs)",
			time.Since(tStart).Seconds())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		scopes.CI.Infof("[Echo Docker]: Building sidecar image")
		tStart := time.Now()
		image, err := imageRegistry.GetOrCreate(sidecarImageName, sidecarBuilder.Build)

		mux.Lock()
		defer mux.Unlock()
		if err != nil {
			aggregateError = multierror.Append(aggregateError, err)
			return
		}

		out.Sidecar = image
		scopes.CI.Infof("[Echo Docker]: Completed building sidecar image (elapsed time: %fs)",
			time.Since(tStart).Seconds())

	}()

	wg.Wait()

	if aggregateError != nil {
		return Instance{}, aggregateError
	}

	scopes.CI.Infof("[Echo Docker]: Completed building images (elapsed time: %fs)",
		time.Since(tStart).Seconds())

	return out, nil
}

func initBuilders() error {
	initMutex.Lock()
	defer initMutex.Unlock()

	if sidecarBuilder != nil && noSidecarBuilder != nil {
		// Already initialized.
		return nil
	}

	scopes.CI.Infof("[Echo Docker]: Creating image builders...")
	tStart := time.Now()

	tmp, err := ioutil.TempDir("", "echo_images")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	// Download the Envoy binary for Linux.
	scopes.CI.Infof("[Echo Docker]: Downloading Envoy SHA %s", envoy.LatestStableSHA)
	envoyStart := time.Now()
	if err := envoy.DownloadLinuxRelease(tmp); err != nil {
		return err
	}
	scopes.CI.Infof("[Echo Docker]: Completed Downloading Envoy (elapsed time: %fs)",
		time.Since(envoyStart).Seconds())

	// Load all of the content into the image builder.
	b := docker.NewImageBuilder().
		AddDir("certs", istioPath(certsDir)).
		AddFile("envoy", filepath.Join(tmp, "usr/local/bin/envoy")).
		AddFile("cert.crt", istioPath(certFile)).
		AddFile("cert.key", istioPath(keyFile)).
		AddFile("postinst.sh", istioPath(postinstScript))

	// Build all of the Go binaries asynchronously.
	wg := &sync.WaitGroup{}
	var aggregateError error
	mux := sync.Mutex{}
	for name, path := range map[string]string{
		"pilot-agent": pilotAgentDir,
		"node_agent":  nodeAgentDir,
		"client":      echoClientDir,
		"server":      echoServerDir,
	} {
		name := name
		path := istioPath(path)
		wg.Add(1)
		go func() {
			defer wg.Done()

			scopes.CI.Infof("[Echo Docker]: Building " + filepath.Base(path))
			tStart := time.Now()
			binaryPath, err := crosscompile.Do(crosscompile.Config{
				SrcRoot: env.IstioSrc,
				SrcPath: path,
				OutDir:  tmp,
			}.LinuxAmd64())

			mux.Lock()
			defer mux.Unlock()

			if err != nil {
				aggregateError = multierror.Append(aggregateError, err)
				return
			}

			b.AddFile(name, binaryPath)

			scopes.CI.Infof("[Echo Docker]: Completed building %s (elapsed time: %fs)", filepath.Base(path),
				time.Since(tStart).Seconds())
		}()
	}
	wg.Wait()

	if aggregateError != nil {
		return aggregateError
	}

	// Add all of the files in the common directory to the tar.
	if err := addGlob(b, istioPath(packagingCommonDir)); err != nil {
		return err
	}

	noSidecarBuilder = b.Copy().
		Tag(noSidecarImageName+":test").
		AddFile("Dockerfile", istioPath(noSidecarDockerFile))
	sidecarBuilder = b.Copy().
		Tag(sidecarImageName+":test").
		AddFile("echo-start.sh", istioPath(sidecarStartScript)).
		AddFile("Dockerfile", istioPath(sidecarDockerFile))

	scopes.CI.Infof("[Echo Docker]: Completed creating image builders (elapsed time: %fs)",
		time.Since(tStart).Seconds())
	return nil
}

func addGlob(b *docker.ImageBuilder, path string) error {
	// Add all of the files in the common directory to the tar.
	files, err := filepath.Glob(filepath.Join(path, "*"))
	if err != nil {
		return err
	}

	for _, f := range files {
		b.AddFile(filepath.Base(f), f)
	}
	return nil
}

func istioPath(relativePath string) string {
	return filepath.Join(env.IstioSrc, relativePath)
}
