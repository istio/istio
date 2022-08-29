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
	"encoding/json"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/compute/metadata"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

const (
	GCPProject           = "gcp_project"
	GCPProjectNumber     = "gcp_project_number"
	GCPCluster           = "gcp_gke_cluster_name"
	GCPClusterURL        = "gcp_gke_cluster_url"
	GCPLocation          = "gcp_location"
	GCEInstance          = "gcp_gce_instance"
	GCEInstanceID        = "gcp_gce_instance_id"
	GCEInstanceTemplate  = "gcp_gce_instance_template"
	GCEInstanceCreatedBy = "gcp_gce_instance_created_by"
	GCPQuotaProject      = "gcp_quota_project"
)

var (
	GCPMetadata = env.Register("GCP_METADATA", "", "Pipe separated GCP metadata, schemed as PROJECT_ID|PROJECT_NUMBER|CLUSTER_NAME|CLUSTER_ZONE").Get()

	// GCPQuotaProjectVar holds the value of the `GCP_QUOTA_PROJECT` environment variable.
	GCPQuotaProjectVar = env.Register("GCP_QUOTA_PROJECT", "", "Allows specification of a quota project to be used in requests to GCP APIs.").Get()
)

var (
	// shouldFillMetadata returns whether the workload is running on GCP and the metadata endpoint is accessible
	// In contrast, DiscoverWithTimeout only checks if the workload is running on GCP
	shouldFillMetadata = func() bool {
		return metadata.OnGCE() && isMetadataEndpointAccessible()
	}
	projectIDFn        = metadata.ProjectID
	numericProjectIDFn = metadata.NumericProjectID
	instanceNameFn     = metadata.InstanceName
	instanceIDFn       = metadata.InstanceID

	clusterNameFn = func() (string, error) {
		cn, err := metadata.InstanceAttributeValue("cluster-name")
		if err != nil {
			return "", err
		}
		return cn, nil
	}
	clusterLocationFn = func() (string, error) {
		cl, err := metadata.InstanceAttributeValue("cluster-location")
		if err == nil {
			return cl, nil
		}
		return metadata.Zone()
	}
	instanceTemplateFn = func() (string, error) {
		it, err := metadata.InstanceAttributeValue("instance-template")
		if err != nil {
			return "", err
		}
		return it, nil
	}
	createdByFn = func() (string, error) {
		cb, err := metadata.InstanceAttributeValue("created-by")
		if err != nil {
			return "", err
		}
		return cb, nil
	}

	constructGKEClusterURL = func(md map[string]string) (string, error) {
		projectID, found := md[GCPProject]
		if !found {
			return "", fmt.Errorf("error constructing GKE cluster url: %s not found in GCP Metadata", GCPProject)
		}
		clusterLocation, found := md[GCPLocation]
		if !found {
			return "", fmt.Errorf("error constructing GKE cluster url: %s not found in GCP Metadata", GCPLocation)
		}
		clusterName, found := md[GCPCluster]
		if !found {
			return "", fmt.Errorf("error constructing GKE cluster url: %s not found in GCP Metadata", GCPCluster)
		}
		return fmt.Sprintf("https://container.googleapis.com/v1/projects/%s/locations/%s/clusters/%s",
			projectID, clusterLocation, clusterName), nil
	}
)

type (
	shouldFillFn     func() bool
	metadataFn       func() (string, error)
	metadataSupplier struct {
		Property string
		Fn       func() (string, error)
	}
)

type gcpEnv struct {
	sync.Mutex
	metadata map[string]string
}

var (
	fillMetadata bool
	gcpEnvOnce   sync.Once
)

// IsGCP returns whether or not the platform for bootstrapping is Google Cloud Platform.
func IsGCP() bool {
	if GCPMetadata != "" {
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
	gcpEnvOnce.Do(func() {
		fillMetadata = shouldFillMetadata()
	})

	md := map[string]string{}
	if e == nil {
		return md
	}
	if GCPMetadata == "" && !fillMetadata {
		return md
	}

	e.Lock()
	defer e.Unlock()
	if e.metadata != nil {
		return e.metadata
	}
	envPid, envNPid, envCN, envLoc := parseGCPMetadata()

	if envPid != "" {
		md[GCPProject] = envPid
	}
	if envNPid != "" {
		md[GCPProjectNumber] = envNPid
	}
	if envLoc != "" {
		md[GCPLocation] = envLoc
	}
	if envCN != "" {
		md[GCPCluster] = envCN
	}

	if fillMetadata {
		// suppliers is an array of functions that supply the metadata for missing properties
		var suppliers []metadataSupplier
		if _, found := md[GCPProject]; !found {
			suppliers = append(suppliers, createMetadataSupplier(GCPProject, projectIDFn))
		}
		if _, found := md[GCPProjectNumber]; !found {
			suppliers = append(suppliers, createMetadataSupplier(GCPProjectNumber, numericProjectIDFn))
		}
		if _, found := md[GCPLocation]; !found {
			suppliers = append(suppliers, createMetadataSupplier(GCPLocation, clusterLocationFn))
		}
		if _, found := md[GCPCluster]; !found {
			suppliers = append(suppliers, createMetadataSupplier(GCPCluster, clusterNameFn))
		}

		wg := waitForMetadataSuppliers(suppliers, md)
		wg.Wait()
	}

	if GCPQuotaProjectVar != "" {
		md[GCPQuotaProject] = GCPQuotaProjectVar
	}
	// Exit early now if not on GCE. This allows setting env var when not on GCE.
	if !fillMetadata {
		e.metadata = md
		return md
	}

	suppliers := []metadataSupplier{
		createMetadataSupplier(GCEInstance, instanceNameFn),
		createMetadataSupplier(GCEInstanceID, instanceIDFn),
		createMetadataSupplier(GCEInstanceTemplate, instanceTemplateFn),
		createMetadataSupplier(GCEInstanceCreatedBy, createdByFn),
	}

	wg := waitForMetadataSuppliers(suppliers, md)
	wg.Wait()

	if clusterURL, err := constructGKEClusterURL(md); err == nil {
		md[GCPClusterURL] = clusterURL
	}
	e.metadata = md
	return md
}

func waitForMetadataSuppliers(suppliers []metadataSupplier, md map[string]string) *sync.WaitGroup {
	wg := sync.WaitGroup{}
	mx := sync.Mutex{}
	for _, mdSupplier := range suppliers {
		wg.Add(1)
		property, supplierFunction := mdSupplier.Property, mdSupplier.Fn
		go func() {
			defer wg.Done()
			if result, err := supplierFunction(); err == nil {
				mx.Lock()
				md[property] = result
				mx.Unlock()
			} else {
				log.Warnf("Error fetching GCP Metadata property %s: %v", property, err)
			}
		}()
	}
	return &wg
}

var (
	parseMetadataOnce sync.Once
	envPid            string
	envNpid           string
	envCluster        string
	envLocation       string
)

func parseGCPMetadata() (pid, npid, cluster, location string) {
	parseMetadataOnce.Do(func() {
		gcpmd := GCPMetadata
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
	if fillMetadata {
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

const ComputeReadonlyScope = "https://www.googleapis.com/auth/compute.readonly"

// Labels attempts to retrieve the GCE instance labels within the timeout
// Requires read access to the Compute API (compute.instances.get)
func (e *gcpEnv) Labels() map[string]string {
	md := e.Metadata()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	success := make(chan bool)
	labels := map[string]string{}
	var instanceLabels map[string]string
	go func() {
		// use explicit credentials with compute.instances.get IAM permissions
		creds, err := google.FindDefaultCredentials(ctx, ComputeReadonlyScope)
		if err != nil {
			log.Warnf("failed to find default credentials: %v", err)
			success <- false
			return
		}
		url := fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s/zones/%s/instances/%s", md[GCPProject], md[GCPLocation], md[GCEInstance])
		resp, err := oauth2.NewClient(ctx, creds.TokenSource).Get(url)
		if err != nil {
			log.Warnf("unable to retrieve instance labels: %v", err)
			success <- false
			return

		}
		defer resp.Body.Close()
		instance := &GcpInstance{}
		if err := json.NewDecoder(resp.Body).Decode(instance); err != nil {
			log.Warnf("failed to decode response: %v", err)
			success <- false
			return
		}
		instanceLabels = instance.Labels
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

// GcpInstance the instances response. Only contains fields we care about, rest are ignored
type GcpInstance struct {
	// Labels: Labels to apply to this instance.
	Labels map[string]string `json:"labels,omitempty"`
}

// Checks metadata to see if GKE metadata or Kubernetes env vars exist
func (e *gcpEnv) IsKubernetes() bool {
	md := e.Metadata()
	_, onKubernetes := os.LookupEnv(KubernetesServiceHost)
	return md[GCPCluster] != "" || onKubernetes
}

func createMetadataSupplier(property string, fn func() (string, error)) metadataSupplier {
	return metadataSupplier{
		Property: property,
		Fn:       fn,
	}
}

func isMetadataEndpointAccessible() bool {
	_, err := net.DialTimeout("tcp", "metadata.google.internal:80", 5*time.Second)
	if err != nil {
		log.Warnf("cannot reach the Google Instance metadata endpoint %v", err)
		return false
	}
	return true
}
