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
	"errors"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"

	"istio.io/istio/pkg/log"

	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

const (
	apiServerRepository = "docker.io/ozevren/galley-testing"
	//apiServerRepository = "gcr.io/oztest-mixer/galley-testing"
	apiServerTag        = "v1"
	connectionRetries   = 10
	retryBackoff        = time.Second * 3
)

var singleton *apiServer
var lock sync.Mutex

// InitAPIServer starts a container with API server. The handle is kept in a package-internal singleton.
// This should be called from a test main to do one-time initialization of the API Server.
func InitAPIServer() (err error) {
	lock.Lock()
	defer lock.Unlock()

	if singleton != nil {
		return
	}

	cmd := exec.Command(
		"docker",
		"pull",
		apiServerRepository + ":" + apiServerTag)
	if err = cmd.Run(); err != nil {
		log.Errorf("Unable to pull docker image: %v", err)
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
func newAPIServer() (*apiServer, error) {
	containerName := fmt.Sprintf("galley-testing-%d", time.Now().UnixNano())

	p, err := findEmptyPort()
	if err != nil {
		log.Errora("Unable to find empty port")
		return nil, err
	}
	localPort := fmt.Sprintf("%d", p)

	cmd := exec.Command(
		"docker",
		"run",
		"-d",
		"--name", containerName,
		"--mount", "type=tmpfs,target=/app,tmpfs-mode=1770",
		"--expose=8080", "-p", "8080:"+localPort,
		apiServerRepository+":"+apiServerTag)

	var b []byte
	if b, err = cmd.Output(); err != nil {
		stderr := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = string(exitErr.Stderr)
		}
		log.Errorf("Could not start docker: %s\n out:\n%s\nerr:\n%s\n", err, string(b), stderr)
		return nil, err
	}

	baseConfig := &rest.Config{
		APIPath:       "/apis",
		ContentConfig: rest.ContentConfig{ContentType: runtime.ContentTypeJSON},
		Host:          "localhost:" + localPort,
	}

	s := &apiServer{
		containerName: containerName,
		baseConfig:    baseConfig,
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
		log.Errorf("Unable to connect to the Api Server, giving up...")
		_ = s.close()
		return nil, errors.New("unable to connect to the Api server, giving up")
	}

	return s, nil
}

// Close purges the container from docker.
func (s *apiServer) close() error {
	cmd := exec.Command("docker", "container", "kill", s.containerName)
	if b, err := cmd.Output(); err != nil {
		log.Errorf("Error running 'docker container kill': %v", err)
		log.Errorf("'docker container kill' output: \n%s\n", string(b))
		return err
	}
	return nil
}

func findEmptyPort() (int, error) {
	basePort := 8080
	for i := 0; i < 20; i++ {
		port := basePort + i
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}

		if err = l.Close(); err != nil {
			continue
		}

		return port, nil
	}

	return 0, errors.New("no available port found")
}
