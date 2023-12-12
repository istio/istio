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

	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/lazy"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/util/sets"
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

// GCPStaticMetadata holds the statically defined GCP metadata
var GCPStaticMetadata = func() map[string]string {
	gcpm := env.Register("GCP_METADATA", "", "Pipe separated GCP metadata, schemed as PROJECT_ID|PROJECT_NUMBER|CLUSTER_NAME|CLUSTER_ZONE").Get()
	quota := env.Register("GCP_QUOTA_PROJECT", "", "Allows specification of a quota project to be used in requests to GCP APIs.").Get()
	if len(gcpm) == 0 {
		return map[string]string{}
	}
	md := map[string]string{}
	parts := strings.Split(gcpm, "|")
	if len(parts) == 4 {
		md[GCPProject] = parts[0]
		md[GCPProjectNumber] = parts[1]
		md[GCPCluster] = parts[2]
		md[GCPLocation] = parts[3]
	}

	if quota != "" {
		md[GCPQuotaProject] = quota
	}

	if clusterURL, err := constructGKEClusterURL(md); err == nil {
		md[GCPClusterURL] = clusterURL
	}
	return md
}()

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
	metadata     map[string]string
	fillMetadata lazy.Lazy[bool]
}

// IsGCP returns whether or not the platform for bootstrapping is Google Cloud Platform.
func IsGCP() bool {
	if len(GCPStaticMetadata) > 0 {
		// Assume this is running on GCP if GCP project env variable is set.
		return true
	}
	return metadata.OnGCE()
}

// NewGCP returns a platform environment customized for Google Cloud Platform.
// Metadata returned by the GCP Environment is taken from the GCE metadata
// service.
func NewGCP() Environment {
	return &gcpEnv{
		fillMetadata: lazy.New(func() (bool, error) {
			return shouldFillMetadata(), nil
		}),
	}
}

func (e *gcpEnv) shouldFillMetadata() bool {
	res, _ := e.fillMetadata.Get()
	return res
}

// Metadata returns GCP environmental data, including project, cluster name, and
// location information.
func (e *gcpEnv) Metadata() map[string]string {
	// If they statically configure metadata, use it immediately and exit. This does limit the ability to configure some static
	// metadata, but extract the rest from the metadata server.
	// However, the motivation to provide static metadata is to remove the dependency on the metadata server, which is unreliable.
	// As a result, it doesn't make much sense to do lookups when this is set.
	// If needed, the remaining pieces of metadata can be added to the static env var (missing is the gce_* ones).
	if len(GCPStaticMetadata) != 0 {
		return GCPStaticMetadata
	}
	// If we cannot reach the metadata server, bail out with only statically defined metadata
	fillMetadata := e.shouldFillMetadata()
	if !fillMetadata {
		return nil
	}

	e.Lock()
	defer e.Unlock()
	// Use previously computed result...
	if e.metadata != nil {
		return e.metadata
	}

	md := map[string]string{}

	// suppliers is an array of functions that supply the metadata for missing properties
	suppliers := []metadataSupplier{
		createMetadataSupplier(GCPProject, projectIDFn),
		createMetadataSupplier(GCPProjectNumber, numericProjectIDFn),
		createMetadataSupplier(GCPLocation, clusterLocationFn),
		createMetadataSupplier(GCPCluster, clusterNameFn),
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
	have := sets.New(maps.Keys(md)...)
	for _, mdSupplier := range suppliers {
		property, supplierFunction := mdSupplier.Property, mdSupplier.Fn
		if have.Contains(property) {
			// We already have this property, we can skip it
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if result, err := supplierFunction(); err == nil {
				mx.Lock()
				md[property] = result
				mx.Unlock()
			} else {
				// Log at debug level as these are often missing (when using GKE metadata server)
				log.Debugf("Error fetching GCP Metadata property %s: %v", property, err)
			}
		}()
	}
	return &wg
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
	loc := e.Metadata()[GCPLocation]
	if loc == "" {
		log.Warnf("Error fetching GCP zone: %v", loc)
		return &l
	}
	r, err := zoneToRegion(loc)
	if err != nil {
		log.Warnf("Error fetching GCP region: %v", err)
		return &l
	}
	return &core.Locality{
		Region:  r,
		Zone:    loc,
		SubZone: "", // GCP has no subzone concept
	}
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

// IsKubernetes checks to see if GKE metadata or Kubernetes env vars exist
func (e *gcpEnv) IsKubernetes() bool {
	_, onKubernetes := os.LookupEnv(KubernetesServiceHost)
	if onKubernetes {
		return true
	}
	return e.Metadata()[GCPCluster] != ""
}

func createMetadataSupplier(property string, fn func() (string, error)) metadataSupplier {
	return metadataSupplier{
		Property: property,
		Fn:       fn,
	}
}

func isMetadataEndpointAccessible() bool {
	// From the Go package, but private so copied here
	const metadataHostEnv = "GCE_METADATA_HOST"
	const metadataIP = "169.254.169.254"
	host := os.Getenv(metadataHostEnv)
	if host == "" {
		host = metadataIP
	}
	_, err := net.DialTimeout("tcp", defaultPort(host, "80"), 5*time.Second)
	if err != nil {
		log.Warnf("cannot reach the Google Instance metadata endpoint %v", err)
		return false
	}
	return true
}

// defaultPort appends the default port, if a port is not already present
func defaultPort(hostMaybePort, dp string) string {
	_, _, err := net.SplitHostPort(hostMaybePort)
	if err != nil {
		return net.JoinHostPort(hostMaybePort, dp)
	}
	return hostMaybePort
}
