/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package leaderelection

import (
	"fmt"
	"io/ioutil"
	"os"

	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
)

const inClusterNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// Options provides the required configuration to create a new resource lock
type Options struct {
	// LeaderElection determines whether or not to use leader election when
	// starting the manager.
	LeaderElection bool

	// LeaderElectionNamespace determines the namespace in which the leader
	// election configmap will be created.
	LeaderElectionNamespace string

	// LeaderElectionID determines the name of the configmap that leader election
	// will use for holding the leader lock.
	LeaderElectionID string
}

// NewResourceLock creates a new config map resource lock for use in a leader
// election loop
func NewResourceLock(config *rest.Config, recorderProvider recorder.Provider, options Options) (resourcelock.Interface, error) {
	if !options.LeaderElection {
		return nil, nil
	}

	// Default the LeaderElectionID
	if options.LeaderElectionID == "" {
		options.LeaderElectionID = "controller-leader-election-helper"
	}

	// Default the namespace (if running in cluster)
	if options.LeaderElectionNamespace == "" {
		var err error
		options.LeaderElectionNamespace, err = getInClusterNamespace()
		if err != nil {
			return nil, fmt.Errorf("unable to find leader election namespace: %v", err)
		}
	}

	// Leader id, needs to be unique
	id, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	id = id + "_" + string(uuid.NewUUID())

	// Construct client for leader election
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	// TODO(JoelSpeed): switch to leaderelection object in 1.12
	return resourcelock.New(resourcelock.ConfigMapsResourceLock,
		options.LeaderElectionNamespace,
		options.LeaderElectionID,
		client.CoreV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: recorderProvider.GetEventRecorderFor(id),
		})
}

func getInClusterNamespace() (string, error) {
	// Check whether the namespace file exists.
	// If not, we are not running in cluster so can't guess the namespace.
	_, err := os.Stat(inClusterNamespacePath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("not running in-cluster, please specify LeaderElectionNamespace")
	} else if err != nil {
		return "", fmt.Errorf("error checking namespace file: %v", err)
	}

	// Load the namespace file and return itss content
	namespace, err := ioutil.ReadFile(inClusterNamespacePath)
	if err != nil {
		return "", fmt.Errorf("error reading namespace file: %v", err)
	}
	return string(namespace), nil
}
