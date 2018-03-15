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

package testing

import (
	"fmt"
	"sync"
	"time"

	"istio.io/istio/galley/pkg/testing/docker"
	"istio.io/istio/pkg/log"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

const (
	apiServerRepository = "docker.io/ozevren/galley-testing"
	apiServerTag        = "v1"
	connectionRetries   = 10
	retryBackoff        = time.Second
)

var singleton *apiServer
var lock sync.Mutex

var pulled bool
var pullLock sync.Mutex

func ensurePull() (err error) {
	pullLock.Lock()
	defer pullLock.Unlock()

	if !pulled {
		if err = docker.Pull(apiServerRepository + ":" + apiServerTag); err != nil {
			return
		}
		pulled = true
	}

	return
}

// InitAPIServer starts a container with API server. The handle is kept in a package-internal singleton.
// This should be called from a test main to do one-time initialization of the API Server.
func InitAPIServer() (err error) {
	if err = ensurePull(); err != nil {
		return err
	}

	lock.Lock()
	defer lock.Unlock()

	if singleton != nil {
		return
	}

	if singleton, err = newAPIServer(); err != nil {
		singleton = nil
	}

	return
}

// ShutdownAPIServer gracefully cleans up the container with the API server. The container is resilient
// to non-graceful terminations: it will self-terminate eventually.
func ShutdownAPIServer() (err error) {
	lock.Lock()
	defer lock.Unlock()

	if singleton == nil {
		return
	}

	err = singleton.close()
	singleton = nil
	return
}

// GetBaseAPIServerConfig returns a base configuration that can be used to connect to the API server that
// is running within the container.
func GetBaseAPIServerConfig() *rest.Config {
	lock.Lock()
	defer lock.Unlock()

	if singleton == nil {
		return nil
	}

	return rest.CopyConfig(singleton.baseConfig)
}

// apiServer represents a Kubernetes Api Server that is launched within a container.
type apiServer struct {
	containerName string
	baseConfig    *rest.Config
}

// newAPIServer returns a new apiServer instance.
func newAPIServer() (s *apiServer, err error) {
	err = ensurePull()
	if err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("galley-testing-%d", time.Now().UnixNano())

	var instance *docker.Instance
	if instance, err = docker.Start(docker.RunArgs{
		Name:    containerName,
		Mount:   "type=tmpfs,target=/app,tmpfs-mode=1770",
		Expose:  "8080",
		Publish: "8080",
		Image:   apiServerRepository + ":" + apiServerTag,
	}); err != nil {
		log.Errorf("Could not start docker: vs", err)
		return nil, err
	}

	defer func() {
		if s == nil {
			// There was an error before the return. Kill the instance.
			_, _ = instance.Kill() // Ignore the error and the output
		}
	}()

	port := ""
	for i := 0; i < connectionRetries; i++ {
		if i != 0 {
			time.Sleep(retryBackoff)
		}

		if port, err = docker.GetExternalPort(containerName, "8080"); err != nil {
			log.Warnf("Error running docker container port: %s", err)
			continue
		}

		break
	}

	if port == "" {
		return nil, err
	}

	baseConfig := &rest.Config{
		APIPath:       "/apis",
		ContentConfig: rest.ContentConfig{ContentType: runtime.ContentTypeJSON},
		Host:          "localhost:" + port,
	}

	completed := false
	for i := 0; i < connectionRetries; i++ {
		if i != 0 {
			time.Sleep(retryBackoff)
		}

		log.Debugf("Attempting to connect to the apiServer...")

		// Try connecting to the custom resource definitions.
		var client *v1beta1.ApiextensionsV1beta1Client
		if client, err = v1beta1.NewForConfig(baseConfig); err != nil {
			log.Debugf("apiServer connection failure: %v", err)
			continue
		}

		if _, err = client.CustomResourceDefinitions().List(v1.ListOptions{}); err != nil {
			log.Debugf("List CustomResourceDefinitions failure: %v", err)
			continue
		}

		completed = true
		break
	}

	if !completed {
		log.Errorf("Unable to connect to the Api Server, giving up: %v", err)
		return nil, fmt.Errorf("unable to connect to the Api server: %v", err)
	}

	err = nil
	s = &apiServer{
		containerName: containerName,
		baseConfig:    baseConfig,
	}

	return
}

// Close purges the container from docker.
func (s *apiServer) close() (err error) {
	if err = docker.Kill(s.containerName); err != nil {
		log.Errorf("Error running 'docker container kill': %v", err)
	}

	return
}
