// Copyright Istio Authors.
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

package cmd

import (
	"context"
	"fmt"
	"regexp"
	"time"

	container "cloud.google.com/go/container/apiv1"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	containerpb "google.golang.org/genproto/googleapis/container/v1"
)

const defaultTimeout = 5 * time.Second

// Returns the cluster defined by the project, location, and cluster name
// Requires container.clusters.get IAM permissions (https://www.googleapis.com/auth/cloud-platform)
func gkeCluster(project, location, clusterName string) (*containerpb.Cluster, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	status := make(chan error)
	var cluster *containerpb.Cluster
	go func() {
		creds, err := google.FindDefaultCredentials(ctx, container.DefaultAuthScopes()[0])
		if err != nil {
			status <- fmt.Errorf("failed to find default credentials: %v", err)
		}
		client, err := container.NewClusterManagerClient(ctx, option.WithCredentials(creds))
		if err != nil {
			status <- fmt.Errorf("failed to create cluster manager client: %v", err)
		}
		req := &containerpb.GetClusterRequest{Name: fmt.Sprintf("projects/%s/locations/%s/clusters/%s", project, location, clusterName)}
		cluster, err = client.GetCluster(ctx, req)
		if err != nil {
			status <- fmt.Errorf("failed to retrieve: %v", err)
		}
		status <- nil
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("fimed out while retrieving the cluster")
	case ok := <-status:
		return cluster, ok
	}
}

// First attempts to infer config (project, location, cluster) from the current kubectl context, then an active pod
func gkeConfig() (string, string, string) {
	project, location, cluster := gkeConfigFromContext()
	if !validGKEConfig(project, location, cluster) {
		project, location, cluster = gkeConfigFromActive()
	}
	return project, location, cluster
}

// Assumes that the kubectl context (created from gcloud container clusters get-credentials) has not been renamed
// Targets contexts in the format "gke_project_location_cluster"
func gkeConfigFromContext() (string, string, string) {
	currentContext := k8sConfig().CurrentContext
	re := regexp.MustCompile("^gke_(.+)_(.+)_(.+)$")
	match := re.FindStringSubmatch(currentContext)
	if len(match) == 4 {
		return match[1], match[2], match[3]
	}
	return "", "", ""
}

// TODO: Remote exec on a pod or pull platform metadata

const (
	DisableLegacyEndpoint = "curl -H 'Metadata-Flavor: Google' 'http://metadata.google.internal/computeMetadata/v1/instance/attributes/disable-legacy-endpoints'"
)

func gkeConfigFromActive() (string, string, string) {
	so, se, err := k8sPodsExec("istiod", "istio-system", DisableLegacyEndpoint, nil)
	if err != nil {
		fmt.Printf("encountered error while getting metadata: %v", err)
	}
	fmt.Printf("%v, %v, %v done", so, se, err)
	return so, se, "dd"
}

func validGKEConfig(project, location, cluster string) bool {
	return project != "" && location != "" && cluster != ""
}
