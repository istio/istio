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

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	cluster2 "istio.io/istio/tools/bug-report/pkg/cluster"
)

// SelectionSpec is a spec for pods that will be Include in the capture
// archive. The format is:
//
//   Namespace1,Namespace2../Deployments/Pods/Label1,Label2.../Annotation1,Annotation2.../ContainerName1,ContainerName2...
//
// Namespace, pod and container names are pattern matching while labels
// and annotations may have pattern in the values with exact match for keys.
// All labels and annotations in the list must match.
// All fields are optional, if they are not specified, all values match.
// Pattern matching style is glob.
// Exclusions have a higher precedence than inclusions.
// Ordering defines pod priority for cases where the archive exceeds the maximum
// size and some logs must be dropped.
//
// Examples:
//
// 1. All pods in test-namespace with label "test=foo" but without label "private" (with any value):
//      include:
//        test-namespace/*/*/test=foo
//      exclude:
//        test-namespace/*/*/private
//
// 2. Pods in all namespaces except "kube-system" with annotation "revision"
// matching wildcard 1.6*:
//      exclude:
//        kube-system/*/*/*/revision=1.6*
//
// 3. Pods with "prometheus" in the name, except those with
// the annotation "internal=true":
//      include:
//        */*/*prometheus*
//      exclude:
//        */*/*prometheus*/*/internal=true
//
// 4. Container logs for all containers called "istio-proxy":
//      include:
//        */*/*/*/*/istio-proxy
//
type SelectionSpec struct {
	Namespaces  []string
	Deployments []string
	Pods        []string
	Containers  []string
	Labels      map[string]string
	Annotations map[string]string
}

// BugReportConfig controls what is captured and Include in the kube-capture tool
// archive.
type BugReportConfig struct {
	// KubeConfigPath is the path to kube config file.
	KubeConfigPath string `json:"kubeConfigPath,omitempty"`
	// Context is the cluster Context in the kube config
	Context string `json:"context,omitempty"`

	// IstioNamespace is the namespace where the istio control plane is installed.
	IstioNamespace string `json:"istioNamespace,omitempty"`

	// DryRun controls whether logs are actually captured and saved.
	DryRun bool `json:"dryRun,omitempty"`

	// FullSecrets controls whether secret contents are included.
	FullSecrets bool `json:"fullSecrets,omitempty"`

	// CommandTimeout is the maximum amount of time running the command
	// before giving up, even if not all logs are captured. Upon timeout,
	// the command creates an archive with only the logs captured so far.
	CommandTimeout Duration `json:"commandTimeout,omitempty"`
	// MaxArchiveSizeMb is the maximum size of the archive in Mb. When
	// the maximum size is reached, logs are selectively discarded based
	// on importance metrics.
	MaxArchiveSizeMb int32 `json:"maxArchiveSizeMb,omitempty"`

	// Include is a list of SelectionSpec entries for resources to include.
	Include []*SelectionSpec `json:"include,omitempty"`
	// Exclude is a list of SelectionSpec entries for resources t0 exclude.
	Exclude []*SelectionSpec `json:"exclude,omitempty"`

	// StartTime is the start time the the log capture time range.
	// If set, Since must be unset.
	StartTime time.Time `json:"startTime,omitempty"`
	// EndTime is the end time the the log capture time range.
	// Default is now.
	EndTime time.Time `json:"endTime,omitempty"`
	// Since defines the start time the the log capture time range.
	// StartTime is set to EndTime - Since.
	// If set, StartTime must be unset.
	Since Duration `json:"since,omitempty"`

	// CriticalErrors is a list of glob pattern matches for errors that,
	// if found in a log, set the highest priority for the log to ensure
	// that it is Include in the capture archive.
	CriticalErrors []string `json:"criticalErrors,omitempty"`
	// IgnoredErrors are glob error patterns which are ignored when
	// calculating the error heuristic for a log.
	IgnoredErrors []string `json:"ignoredErrors,omitempty"`

	// GCSURL is an URL to the GCS bucket where the archive is uploaded.
	GCSURL string `json:"gcsURL,omitempty"`
	// UploadToGCS uploads the archive to a GCS bucket. If no bucket
	// address is given, it creates one.
	UploadToGCS bool `json:"uploadToGCS,omitempty"`
}

func parseToIncludeTypeSlice(s string) []string {
	if strings.TrimSpace(s) == "*" {
		return nil
	}
	return strings.Split(s, ",")
}

func parseToIncludeTypeMap(s string) (map[string]string, error) {
	if strings.TrimSpace(s) == "*" {
		return nil, nil
	}
	out := make(map[string]string)
	for _, ss := range strings.Split(s, ",") {
		if len(ss) == 0 {
			continue
		}
		kv := strings.Split(ss, "=")
		if len(kv) != 2 {
			return nil, fmt.Errorf("bad label/annotation selection %s, must have format key=value", ss)
		}
		out[kv[0]] = kv[1]
	}
	return out, nil
}

func (s *SelectionSpec) UnmarshalJSON(b []byte) error {
	ft := []cluster2.ResourceType{cluster2.Namespace, cluster2.Deployment, cluster2.Pod, cluster2.Label, cluster2.Annotation, cluster2.Container}
	str := strings.TrimPrefix(strings.TrimSuffix(string(b), `"`), `"`)
	for i, f := range strings.Split(str, "/") {
		var err error
		switch ft[i] {
		case cluster2.Namespace:
			s.Namespaces = parseToIncludeTypeSlice(f)
			if err != nil {
				return err
			}
		case cluster2.Deployment:
			s.Deployments = parseToIncludeTypeSlice(f)
			if err != nil {
				return err
			}
		case cluster2.Pod:
			s.Pods = parseToIncludeTypeSlice(f)
			if err != nil {
				return err
			}
		case cluster2.Label:
			s.Labels, err = parseToIncludeTypeMap(f)
			if err != nil {
				return err
			}
		case cluster2.Annotation:
			s.Annotations, err = parseToIncludeTypeMap(f)
			if err != nil {
				return err
			}
		case cluster2.Container:
			s.Containers = parseToIncludeTypeSlice(f)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(tmp)
		return nil
	default:
		return errors.New("invalid duration")
	}
}
