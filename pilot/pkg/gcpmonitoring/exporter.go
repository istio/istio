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
	"errors"
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
	"istio.io/pkg/log"
	"istio.io/pkg/version"
)

const (
	authScope = "https://www.googleapis.com/auth/cloud-platform"
)

var (
	trustDomain  = ""
	podName      = ""
	podNamespace = ""
	meshUID      = ""
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
		if pid, ok := gcpMetadata[platform.GCPProjectNumber]; ok && pid != "" {
			meshUID = "proj-" + pid
		}
	}
	labels.Set("mesh_uid", meshUID, "ID for Mesh")
	labels.Set("revision", version.Info.Version, "Control plane revision")
	clientOptions := []option.ClientOption{}
	if strings.HasSuffix(trustDomain, "svc.id.goog") {
		// Workload identity is enabled and P4SA access token is used.
		if subjectToken, err := ioutil.ReadFile(model.K8sSATrustworthyJwtFileName); err == nil {
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
	se, err := stackdriver.NewExporter(stackdriver.Options{
		MetricPrefix:            "istio.io/control",
		MonitoringClientOptions: clientOptions,
		GetMetricType: func(view *view.View) string {
			return "istio.io/control/" + view.Name
		},
		MonitoredResource: &monitoredresource.GKEContainer{
			ProjectID:                  gcpMetadata[platform.GCPProject],
			ClusterName:                gcpMetadata[platform.GCPCluster],
			Zone:                       gcpMetadata[platform.GCPLocation],
			NamespaceID:                podNamespace,
			PodID:                      podName,
			ContainerName:              "discovery",
			LoggingMonitoringV2Enabled: true,
		},
		DefaultMonitoringLabels: labels,
		ReportingInterval:       60 * time.Second,
	})

	if err != nil {
		return nil, errors.New("fail to initialize Stackdriver exporter")
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
	} else {
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
