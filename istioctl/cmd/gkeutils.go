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

	"istio.io/pkg/log"

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
// Returns the config along with the relevant cluster
func gkeConfig() (project, location, clusterName string, cluster *containerpb.Cluster, err error) {
	project, location, clusterName = gkeConfigFromContext()
	cluster, err = gkeCluster(project, location, clusterName)
	if err == nil {
		log.Debugf("inferred (project, location, cluster) from kubectl context: (%s, %s, %s)\n", project, location, clusterName)
		return
	}

	project, location, clusterName = gkeConfigFromActive()
	cluster, err = gkeCluster(project, location, clusterName)
	if err == nil {
		log.Debugf("inferred (project, location, cluster) from metadata server: (%s, %s, %s)\n", project, location, clusterName)
	}
	return
}

// Assumes that the kubectl context (created from gcloud container clusters get-credentials) has not been renamed
// Targets contexts in the format "gke_project_location_cluster"
func gkeConfigFromContext() (project, location, cluster string) {
	currentContext := k8sConfig().CurrentContext
	re := regexp.MustCompile("^gke_(.+)_(.+)_(.+)$")
	match := re.FindStringSubmatch(currentContext)
	if len(match) == 4 {
		return match[1], match[2], match[3]
	}
	return
}

const (
	MetadataCurlCmd         = "curl -H Metadata-Flavor:Google %s"
	ProjectIDEndpoint       = "http://metadata.google.internal/computeMetadata/v1/project/project-id"
	ClusterLocationEndpoint = "http://metadata.google.internal/computeMetadata/v1/instance/attributes/cluster-location"
	ClusterNameEndpoint     = "http://metadata.google.internal/computeMetadata/v1/instance/attributes/cluster-name"
)

// Attempts to access the metadata API through the istiod pod
// Requires gcloud application credentials (gcloud auth application-default login)
func gkeConfigFromActive() (project, location, cluster string) {
	project, _, err := k8sPodsExec("istiod", "istio-system", fmt.Sprintf(MetadataCurlCmd, ProjectIDEndpoint), nil)
	if err != nil {
		fmt.Printf("encountered error while getting metadata: %v\n", err)
		return
	}
	location, _, err = k8sPodsExec("istiod", "istio-system", fmt.Sprintf(MetadataCurlCmd, ClusterLocationEndpoint), nil)
	if err != nil {
		fmt.Printf("encountered error while getting metadata: %v\n", err)
		return
	}
	cluster, _, err = k8sPodsExec("istiod", "istio-system", fmt.Sprintf(MetadataCurlCmd, ClusterNameEndpoint), nil)
	if err != nil {
		fmt.Printf("encountered error while getting metadata: %v\n", err)
		return
	}
	return project, location, cluster
}
