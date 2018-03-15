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
	"sync"

	"gopkg.in/ory-am/dockertest.v3"
	"istio.io/istio/pkg/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
)

const (
	apiServerRepository = "docker.io/ozevren/galley-testing"
	apiServerTag        = "v1"
	apiServerPort       = "8080/tcp"
)

var singleton *apiServer
var lock sync.Mutex

// InitApiServer starts a container with API server. The handle is kept in a package-internal singleton.
// This should be called from a test main to do one-time initialization of the API Server.
func InitApiServer() (err error) {
	lock.Lock()
	defer lock.Unlock()

	if singleton != nil {
		return
	}

	if singleton, err = newApiServer(); err != nil {
		singleton = nil
	}

	return
}

// ShutdownApiServer gracefully cleans up the container with the API server. The container is resilient
// to non-graceful terminations: it will self-terminate eventually.
func ShutdownApiServer() (err error) {
	lock.Lock()
	defer lock.Unlock()

	if singleton == nil {
		return
	}

	err = singleton.close()
	singleton = nil
	return
}

// GetBaseApiServerConfig returns a base configuration that can be used to connect to the API server that
// is running within the container.
func GetBaseApiServerConfig() *rest.Config {
	lock.Lock()
	defer lock.Unlock()

	if singleton == nil {
		return nil
	}

	return rest.CopyConfig(singleton.baseConfig)
}

// apiServer represents a Kubernetes Api Server that is launched within a container.
type apiServer struct {
	dockerPool *dockertest.Pool
	container  *dockertest.Resource
	baseConfig *rest.Config
}

// newApiServer returns a new apiServer instance.
func newApiServer() (*apiServer, error) {

	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Errorf("Could not connect to docker: %s", err)
		return nil, err
	}

	// pulls an image, creates a container based on it and runs it
	container, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: apiServerRepository,
		Tag: apiServerTag,
	})
	if err != nil {
		log.Errorf("Could not start container: %s", err)
		return nil, err
	}

	port := container.GetPort(apiServerPort)
	log.Debugf("apiServer container port is: %s", port)

	baseConfig := &rest.Config{
		APIPath:       "/apis",
		ContentConfig: rest.ContentConfig{ContentType: runtime.ContentTypeJSON},
		Host:          "localhost:" + port,
	}

	s := &apiServer{
		dockerPool: pool,
		container:  container,
		baseConfig: baseConfig,
	}

	err = pool.Retry(func() error {
		log.Debugf("Attempting to connect to the apiServer...")

		// Try connecting to the custom resource definitions.
		var client *v1beta1.ApiextensionsV1beta1Client
		if client, err = v1beta1.NewForConfig(baseConfig); err != nil {
			log.Debugf("apiServer connection failure: %v", err)
			return err
		}

		if _, err = client.CustomResourceDefinitions().List(v1.ListOptions{}); err != nil {
			log.Debugf("List CustomResourceDefinitions failure: %v", err)
			return err
		}

		return nil
	})

	if err != nil {
		_ = s.close() // retry error takes precedence
		log.Errorf("Could not connect to docker: %s", err)
		return nil, err
	}

	return s, nil
}

// Close purges the container from docker.
func (s *apiServer) close() error {
	if err := s.dockerPool.Purge(s.container); err != nil {
		log.Errorf("Could not purge resource: %s", err)
		return err
	}

	return nil
}
