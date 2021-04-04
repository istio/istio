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

package gcpmonitoring

import (
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	ocprom "contrib.go.opencensus.io/exporter/prometheus"
	"contrib.go.opencensus.io/exporter/stackdriver"
	"contrib.go.opencensus.io/exporter/stackdriver/monitoredresource"
	"go.opencensus.io/stats/view"
	"google.golang.org/api/option"

	"istio.io/istio/pilot/pkg/security/model"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/security/pkg/stsservice/tokenmanager"
	"istio.io/istio/security/pkg/util"
	"istio.io/pkg/env"
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	authScope                 = "https://www.googleapis.com/auth/cloud-platform"
	workloadIdentitySuffix    = "svc.id.goog"
	hubWorkloadIdentitySuffix = "hub.id.goog"
)

var (
	trustDomain  = ""
	podName      = ""
	podNamespace = ""
	meshUID      = ""

	cloudRunServiceVar = env.RegisterStringVar("K_SERVICE", "", "cloud run service name")
	managedRevisionVar = env.RegisterStringVar("REV", "", "name of the managed revision, e.g. asm, asmca, ossmanaged")
)

// ASMExporter is a stats exporter used for ASM control plane metrics.
// It wraps a prometheus exporter and a stackdriver exporter, and exports two types of views.
type ASMExporter struct {
	PromExporter *ocprom.Exporter
	sdExporter   *stackdriver.Exporter
}

// SetTrustDomain sets GCP trust domain, which is used to fetch GCP metrics.
// Use this function instead of passing trust domain string around to avoid conflicting with OSS changes.
func SetTrustDomain(td string) {
	trustDomain = td
}

// SetPodName sets k8s pod name, which is used in metrics monitored resource.
func SetPodName(pn string) {
	podName = pn
}

// SetPodNamespace sets k8s pod namesapce, which is used in metrics monitored resource.
func SetPodNamespace(pn string) {
	podNamespace = pn
}

// SetMeshUID sets UID of the mesh that this control plane runs in.
func SetMeshUID(uid string) {
	meshUID = uid
}

// NewASMExporter creates an ASM opencensus exporter.
func NewASMExporter(pe *ocprom.Exporter) (*ASMExporter, error) {
	if !enableSDVar.Get() {
		// Stackdriver monitoring is not enabled, return early with only prometheus exporter initialized.
		return &ASMExporter{
			PromExporter: pe,
		}, nil
	}
	labels := &stackdriver.Labels{}
	gcpMetadata := platform.NewGCP().Metadata()
	if meshUID == "" {
		meshUID = meshUIDFromPlatformMeta(gcpMetadata)
	}
	labels.Set("mesh_uid", meshUID, "ID for Mesh")
	labels.Set("revision", revisionLabel(), "Control plane revision")
	labels.Set("control_plane_version", version.Info.Version, "Control plane version")
	clientOptions := []option.ClientOption{}

	if !isCloudRun() {

		// The audience from the token should tell us (in addition to the trust domain)
		// whether or not we should do the token exchange.
		// It's fine to fail for reading from the token because we will use the trust domain as a fallback.
		subjectToken, err := ioutil.ReadFile(model.K8sSATrustworthyJwtFileName)
		var audience string
		if err == nil {
			if audiences, _ := util.GetAud(string(subjectToken)); len(audiences) == 1 {
				audience = audiences[0]
			}
		}

		// Do the token exchange if either the trust domain or the audience of the token has the workload identity suffix.
		// The trust domain may not have the workload identity suffix if the user changes to use a different trust domain
		// but the audience of the token should always have if the workload identity is enabled, otherwise it means something
		// wrong in the installation.
		if strings.HasSuffix(trustDomain, workloadIdentitySuffix) || strings.HasSuffix(audience, workloadIdentitySuffix) ||
			strings.HasSuffix(trustDomain, hubWorkloadIdentitySuffix) || strings.HasSuffix(audience, hubWorkloadIdentitySuffix) {
			// Workload identity is enabled and P4SA access token is used.
			if err == nil {
				ts := tokenmanager.NewTokenSource(trustDomain, string(subjectToken), authScope)
				clientOptions = append(clientOptions, option.WithTokenSource(ts), option.WithQuotaProject(gcpMetadata[platform.GCPProject]))
				// Set up goroutine to read token file periodically and refresh subject token with new expiry.
				go func() {
					for range time.Tick(5 * time.Minute) {
						if subjectToken, err := ioutil.ReadFile(model.K8sSATrustworthyJwtFileName); err == nil {
							ts.RefreshSubjectToken(string(subjectToken))
						} else {
							log.Debugf("Cannot refresh subject token for sts token source: %v", err)
						}
					}
				}()
			} else {
				log.Errorf("Cannot read third party jwt token file: %v", err)
			}
		}
	}

	mr := &monitoredresource.GKEContainer{
		ProjectID:                  gcpMetadata[platform.GCPProject],
		ClusterName:                gcpMetadata[platform.GCPCluster],
		Zone:                       gcpMetadata[platform.GCPLocation],
		NamespaceID:                podNamespace,
		PodID:                      podName,
		ContainerName:              "discovery",
		LoggingMonitoringV2Enabled: true,
	}
	if isCloudRun() {
		mr.ContainerName = "cr-" + managedRevisionVar.Get()
	}
	se, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:               gcpMetadata[platform.GCPProject],
		Location:                gcpMetadata[platform.GCPLocation],
		MetricPrefix:            "istio.io/control",
		MonitoringClientOptions: clientOptions,
		TraceClientOptions:      clientOptions,
		GetMetricType: func(view *view.View) string {
			return "istio.io/control/" + view.Name
		},
		MonitoredResource:       mr,
		DefaultMonitoringLabels: labels,
		ReportingInterval:       60 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("fail to initialize Stackdriver exporter: %v", err)
	}

	if isCloudRun() {
		return &ASMExporter{
			sdExporter: se,
		}, nil
	}
	return &ASMExporter{
		PromExporter: pe,
		sdExporter:   se,
	}, nil
}

// ExportView exports all views collected by control plane process.
// This function distinguished views for Stackdriver and views for Prometheus and exporting them separately.
func (e *ASMExporter) ExportView(vd *view.Data) {
	if _, ok := viewMap[vd.View.Name]; ok && e.sdExporter != nil {
		// This indicates that this is a stackdriver view
		e.sdExporter.ExportView(vd)
	} else if e.PromExporter != nil {
		// nolint: staticcheck
		e.PromExporter.ExportView(vd)
	}
}

// TestExporter is used for GCP monitoring test.
type TestExporter struct {
	sync.Mutex

	Rows        map[string][]*view.Row
	invalidTags bool
}

// ExportView exports test views.
func (t *TestExporter) ExportView(d *view.Data) {
	t.Lock()
	defer t.Unlock()
	for _, tk := range d.View.TagKeys {
		if len(tk.Name()) < 1 {
			t.invalidTags = true
		}
	}
	t.Rows[d.View.Name] = append(t.Rows[d.View.Name], d.Rows...)
}

func isCloudRun() bool {
	if svc := cloudRunServiceVar.Get(); svc != "" {
		return true
	}
	return false
}

func revisionLabel() string {
	if isCloudRun() {
		return managedRevisionVar.Get()
	}
	return version.Info.Version
}
