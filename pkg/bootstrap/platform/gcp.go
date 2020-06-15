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
	instanceNameFn = func() (string, error) {
		in, err := metadata.InstanceName()
		if err != nil {
			return "", err
		}
		return in, nil
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
)

type shouldFillFn func() bool
type metadataFn func() (string, error)

type gcpEnv struct {
	shouldFillMetadata shouldFillFn
	projectIDFn        metadataFn
	numericProjectIDFn metadataFn
	locationFn         metadataFn
	clusterNameFn      metadataFn
	instanceNameFn     metadataFn
	instanceIDFn       metadataFn
	instanceTemplateFn metadataFn
	createdByFn        metadataFn
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
	return &gcpEnv{
		shouldFillMetadata: metadata.OnGCE,
		projectIDFn:        metadata.ProjectID,
		numericProjectIDFn: metadata.NumericProjectID,
		locationFn:         clusterLocationFn,
		clusterNameFn:      clusterNameFn,
		instanceNameFn:     instanceNameFn,
		instanceIDFn:       metadata.InstanceID,
		instanceTemplateFn: instanceTemplateFn,
		createdByFn:        createdByFn,
	}
}

// Metadata returns GCP environmental data, including project, cluster name, and
// location information.
func (e *gcpEnv) Metadata() map[string]string {
	md := map[string]string{}
	if e == nil {
		return md
	}
	if gcpMetadataVar.Get() == "" && !e.shouldFillMetadata() {
		return md
	}
	envPid, envNPid, envCN, envLoc := parseGCPMetadata()
	if envPid != "" {
		md[GCPProject] = envPid
	} else if pid, err := e.projectIDFn(); err == nil {
		md[GCPProject] = pid
	}
	if envNPid != "" {
		md[GCPProjectNumber] = envNPid
	} else if npid, err := e.numericProjectIDFn(); err == nil {
		md[GCPProjectNumber] = npid
	}
	if envLoc != "" {
		md[GCPLocation] = envLoc
	} else if l, err := e.locationFn(); err == nil {
		md[GCPLocation] = l
	}
	if envCN != "" {
		md[GCPCluster] = envCN
	} else if cn, err := e.clusterNameFn(); err == nil {
		md[GCPCluster] = cn
	}
	if in, err := e.instanceNameFn(); err == nil {
		md[GCEInstance] = in
	}
	if id, err := e.instanceIDFn(); err == nil {
		md[GCEInstanceID] = id
	}
	if it, err := e.instanceTemplateFn(); err == nil {
		md[GCEInstanceTemplate] = it
	}
	if cb, err := e.createdByFn(); err == nil {
		md[GCEInstanceCreatedBy] = cb
	}
	return md
}

var (
	once        sync.Once
	envPid      string
	envNpid     string
	envCluster  string
	envLocation string
)

func parseGCPMetadata() (pid, npid, cluster, location string) {
	once.Do(func() {
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

// Labels attempts to retrieve the GCE instance labels from provided metadata within the timeout
func (e *gcpEnv) Labels() map[string]string {
	project, _ := e.projectIDFn()
	zone, _ := e.locationFn()
	instance, _ := e.instanceNameFn()

	labels := map[string]string{}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	// use explicit credentials with compute.instances.get IAM permissions
	creds, err := google.FindDefaultCredentials(ctx, compute.CloudPlatformScope)
	if err != nil {
		log.Warnf("failed to find default credentials: %v", err)
		labels["err"] = fmt.Sprintf("failed to find default credentials: %v", err)
		return labels
	}
	computeService, err := compute.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		log.Warnf("failed to create new service: %v", err)
		labels["err"] = fmt.Sprintf("failed to create new service: %v", err)
		return labels
	}

	success := make(chan bool)
	go func() {
		instance, err := computeService.Instances.Get(project, zone, instance).Do()
		if err != nil {
			log.Warnf("unable to retrieve instance: %v", err)
			labels["err"] = fmt.Sprintf("unable to retrieve instance: %v", err)
			success <- false
		} else {
			labels = instance.Labels
			success <- true
		}
	}()
	select {
	case <-ctx.Done():
		log.Warnf("context deadline exceeded for instance get request: %v", ctx.Err())
		labels["err"] = fmt.Sprintf("context deadline exceeded for instance get request: %v", ctx.Err())
	case <-success:
		labels["status"] = "success"
	}
	return labels
}
