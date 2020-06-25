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

package platform

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"cloud.google.com/go/compute/metadata"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	GCPProject           = "gcp_project"
	GCPProjectNumber     = "gcp_project_number"
	GCPCluster           = "gcp_gke_cluster_name"
	GCPLocation          = "gcp_location"
	GCEInstance          = "gcp_gce_instance"
	GCEInstanceID        = "gcp_gce_instance_id"
	GCEInstanceTemplate  = "gcp_gce_instance_template"
	GCEInstanceCreatedBy = "gcp_gce_instance_created_by"
)

var (
	gcpMetadataVar = env.RegisterStringVar("GCP_METADATA", "", "Pipe separted GCP metadata, schemed as PROJECT_ID|PROJECT_NUMBER|CLUSTER_NAME|CLUSTER_ZONE")
)

var (
	clusterNameFn = func(e *gcpEnv) (string, error) {
		f := func() (string, error) { return metadata.InstanceAttributeValue("cluster-name") }
		return getCacheMetadata(&e.clusterName, f)
	}
	clusterLocationFn = func(e *gcpEnv) (string, error) {
		f := func() (string, error) { return metadata.InstanceAttributeValue("cluster-location") }
		if location, err := getCacheMetadata(&e.location, f); err == nil {
			return location, nil
		}
		return getCacheMetadata(&e.location, metadata.Zone)
	}
	instanceNameFn = func(e *gcpEnv) (string, error) {
		return getCacheMetadata(&e.instanceName, metadata.InstanceName)
	}
	instanceTemplateFn = func(e *gcpEnv) (string, error) {
		f := func() (string, error) { return metadata.InstanceAttributeValue("instance-template") }
		return getCacheMetadata(&e.instanceTemplate, f)
	}
	createdByFn = func(e *gcpEnv) (string, error) {
		f := func() (string, error) { return metadata.InstanceAttributeValue("created-by") }
		return getCacheMetadata(&e.instanceTemplate, f)
	}
	shouldFillMetadata = metadata.OnGCE
	projectIDFn        = func(e *gcpEnv) (string, error) {
		return getCacheMetadata(&e.projectID, metadata.ProjectID)
	}
	numericProjectIDFn = func(e *gcpEnv) (string, error) {
		return getCacheMetadata(&e.numericProjectID, metadata.NumericProjectID)
	}
	instanceIDFn = func(e *gcpEnv) (string, error) {
		return getCacheMetadata(&e.instanceID, metadata.InstanceID)
	}
)

type shouldFillFn func() bool
type metadataFn func(e *gcpEnv) (string, error)

type gcpEnv struct {
	projectID        string
	numericProjectID string
	location         string
	clusterName      string
	instanceName     string
	instanceID       string
	instanceTemplate string
	createdBy        string
}

// Helper for compute metadata getters. Caches and returns f() if valid.
func getCacheMetadata(s *string, f func() (string, error)) (string, error) {
	if *s == "" {
		attr, err := f()
		if err != nil {
			return "", err
		}
		*s = attr
	}
	return *s, nil
}

// IsGCP returns whether or not the platform for bootstrapping is Google Cloud Platform.
func IsGCP() bool {
	if gcpMetadataVar.Get() != "" {
		// Assume this is running on GCP if GCP project env variable is set.
		return true
	}
	return metadata.OnGCE()
}

// NewGCP returns a platform environment customized for Google Cloud Platform.
// Metadata returned by the GCP Environment is taken from the GCE metadata
// service.
func NewGCP() Environment {
	return &gcpEnv{}
}

// Metadata returns GCP environmental data, including project, cluster name, and
// location information.
func (e *gcpEnv) Metadata() map[string]string {
	md := map[string]string{}
	if e == nil {
		return md
	}
	if gcpMetadataVar.Get() == "" && !shouldFillMetadata() {
		return md
	}
	envPid, envNPid, envCN, envLoc := parseGCPMetadata()
	// Cache environment variables into GCPEnv over metadata
	if envPid != "" {
		md[GCPProject] = envPid
		e.projectID = envPid
	} else if pid, err := projectIDFn(e); err == nil {
		md[GCPProject] = pid
	}
	if envNPid != "" {
		md[GCPProjectNumber] = envNPid
		e.numericProjectID = envNPid
	} else if npid, err := numericProjectIDFn(e); err == nil {
		md[GCPProjectNumber] = npid
	}
	if envLoc != "" {
		md[GCPLocation] = envLoc
		e.location = envLocation
	} else if l, err := clusterLocationFn(e); err == nil {
		md[GCPLocation] = l
	}
	if envCN != "" {
		md[GCPCluster] = envCN
		e.clusterName = envCN
	} else if cn, err := clusterNameFn(e); err == nil {
		md[GCPCluster] = cn
	}
	if in, err := instanceNameFn(e); err == nil {
		md[GCEInstance] = in
	}
	if id, err := instanceIDFn(e); err == nil {
		md[GCEInstanceID] = id
	}
	if it, err := instanceTemplateFn(e); err == nil {
		md[GCEInstanceTemplate] = it
	}
	if cb, err := createdByFn(e); err == nil {
		md[GCEInstanceCreatedBy] = cb
	}
	return md
}

var (
	envOnce     sync.Once
	envPid      string
	envNpid     string
	envCluster  string
	envLocation string
)

func parseGCPMetadata() (pid, npid, cluster, location string) {
	envOnce.Do(func() {
		gcpmd := gcpMetadataVar.Get()
		if len(gcpmd) > 0 {
			log.Infof("Extract GCP metadata from env variable GCP_METADATA: %v", gcpmd)
			parts := strings.Split(gcpmd, "|")
			if len(parts) == 4 {
				envPid = parts[0]
				envNpid = parts[1]
				envCluster = parts[2]
				envLocation = parts[3]
			}
		}
	})
	return envPid, envNpid, envCluster, envLocation
}

// Converts a GCP zone into a region.
func zoneToRegion(z string) (string, error) {
	// Zones are in the form <region>-<zone_suffix>, so capture everything but the suffix.
	re := regexp.MustCompile("(.*)-.*")
	m := re.FindStringSubmatch(z)
	if len(m) != 2 {
		return "", fmt.Errorf("unable to extract region from GCP zone: %s", z)
	}
	return m[1], nil
}

// Locality returns the GCP-specific region and zone.
func (e *gcpEnv) Locality() *core.Locality {
	var l core.Locality
	if metadata.OnGCE() {
		z, zerr := metadata.Zone()
		if zerr != nil {
			log.Warnf("Error fetching GCP zone: %v", zerr)
			return &l
		}
		r, rerr := zoneToRegion(z)
		if rerr != nil {
			log.Warnf("Error fetching GCP region: %v", rerr)
			return &l
		}
		l.Region = r
		l.Zone = z
	}

	return &l
}

// Labels attempts to retrieve the GCE instance labels within the timeout
// Requires read access to the Compute API (compute.instances.get)
func (e *gcpEnv) Labels() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	success := make(chan bool)
	labels := map[string]string{}
	var instanceLabels map[string]string
	go func() {
		// use explicit credentials with compute.instances.get IAM permissions
		creds, err := google.FindDefaultCredentials(ctx, compute.CloudPlatformScope)
		if err != nil {
			log.Warnf("failed to find default credentials: %v", err)
			success <- false
			return
		}
		computeService, err := compute.NewService(ctx, option.WithCredentials(creds))
		if err != nil {
			log.Warnf("failed to create new service: %v", err)
			success <- false
			return
		}
		// instance.Labels is nil if no labels are present
		projectID, _ := projectIDFn(e)
		location, _ := clusterLocationFn(e)
		instanceName, _ := instanceNameFn(e)
		instanceObj, err := computeService.Instances.Get(projectID, location, instanceName).Do()
		if err != nil {
			log.Warnf("unable to retrieve instance: %v", err)
			success <- false
			return
		}
		instanceLabels = instanceObj.Labels
		success <- true
	}()
	select {
	case <-ctx.Done():
		log.Warnf("context deadline exceeded for instance get request: %v", ctx.Err())
	case ok := <-success:
		if ok && instanceLabels != nil {
			labels = instanceLabels
		}
	}
	return labels
}

const KubernetesServiceHost = "KUBERNETES_SERVICE_HOST"

// Checks metadata to see if GKE metadata or Kubernetes env vars exist
func (e *gcpEnv) IsKubernetes() bool {
	clusterName, _ := clusterNameFn(e)
	_, onKubernetes := os.LookupEnv(KubernetesServiceHost)
	return clusterName != "" || onKubernetes
}
