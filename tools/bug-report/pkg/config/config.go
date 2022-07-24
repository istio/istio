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
	"math"
	"strings"
	"time"
)

type ResourceType int

const (
	Namespace ResourceType = iota
	Deployment
	Pod
	Label
	Annotation
	Container
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
	Namespaces  []string          `json:"namespaces,omitempty"`
	Deployments []string          `json:"deployments,omitempty"`
	Pods        []string          `json:"pods,omitempty"`
	Containers  []string          `json:"containers,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type SelectionSpecs []*SelectionSpec

func (s SelectionSpecs) String() string {
	var out []string
	for _, ss := range s {
		st := ""
		if !defaultListSetting(ss.Namespaces) {
			st += fmt.Sprintf("Namespaces: %s", strings.Join(ss.Namespaces, ","))
		}
		if !defaultListSetting(ss.Deployments) {
			st += fmt.Sprintf("/Deployments: %s", strings.Join(ss.Deployments, ","))
		}
		if !defaultListSetting(ss.Pods) {
			st += fmt.Sprintf("/Pods:%s", strings.Join(ss.Pods, ","))
		}
		if !defaultListSetting(ss.Containers) {
			st += fmt.Sprintf("/Containers: %s", strings.Join(ss.Containers, ","))
		}
		if len(ss.Labels) > 0 {
			st += fmt.Sprintf("/Labels: %v", ss.Labels)
		}
		if len(ss.Annotations) > 0 {
			st += fmt.Sprintf("/Annotations: %v", ss.Annotations)
		}
		out = append(out, "{ "+st+" }")
	}
	return strings.Join(out, " AND ")
}

func defaultListSetting(s []string) bool {
	if len(s) < 1 {
		return true
	}
	if len(s) == 1 {
		return strings.TrimSpace(s[0]) == "" || s[0] == "*"
	}
	return false
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

	// Include is a list of SelectionSpec entries for resources to include.
	Include SelectionSpecs `json:"include,omitempty"`
	// Exclude is a list of SelectionSpec entries for resources t0 exclude.
	Exclude SelectionSpecs `json:"exclude,omitempty"`

	// StartTime is the start time the log capture time range.
	// If set, Since must be unset.
	StartTime time.Time `json:"startTime,omitempty"`
	// EndTime is the end time the log capture time range.
	// Default is now.
	EndTime time.Time `json:"endTime,omitempty"`
	// Since defines the start time the log capture time range.
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
}

func (b *BugReportConfig) String() string {
	out := ""
	if b.KubeConfigPath != "" {
		out += fmt.Sprintf("kubeconfig: %s\n", b.KubeConfigPath)
	}
	if b.Context != "" {
		out += fmt.Sprintf("context: %s\n", b.Context)
	}
	out += fmt.Sprintf("istio-namespace: %s\n", b.IstioNamespace)
	out += fmt.Sprintf("full-secrets: %v\n", b.FullSecrets)
	out += fmt.Sprintf("timeout (mins): %v\n", math.Round(float64(int(b.CommandTimeout))/float64(time.Minute)))
	out += fmt.Sprintf("include: %s\n", b.Include)
	out += fmt.Sprintf("exclude: %s\n", b.Exclude)
	if !b.StartTime.Equal(time.Time{}) {
		out += fmt.Sprintf("start-time: %v\n", b.StartTime)
	}
	out += fmt.Sprintf("end-time: %v\n", b.EndTime)
	if b.Since != 0 {
		out += fmt.Sprintf("since: %v\n", b.Since)
	}
	return out
}

func parseToIncludeTypeSlice(s string) []string {
	if strings.TrimSpace(s) == "*" || s == "" {
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
		if strings.Contains(kv[0], "*") {
			return nil, fmt.Errorf("bad label/annotation selection %s, key cannot have '*' wildcards", ss)
		}
		out[kv[0]] = kv[1]
	}
	return out, nil
}

func (s *SelectionSpec) UnmarshalJSON(b []byte) error {
	ft := []ResourceType{Namespace, Deployment, Pod, Label, Annotation, Container}
	str := strings.TrimPrefix(strings.TrimSuffix(string(b), `"`), `"`)
	for i, f := range strings.Split(str, "/") {
		var err error
		switch ft[i] {
		case Namespace:
			s.Namespaces = parseToIncludeTypeSlice(f)
		case Deployment:
			s.Deployments = parseToIncludeTypeSlice(f)
		case Pod:
			s.Pods = parseToIncludeTypeSlice(f)
		case Label:
			s.Labels, err = parseToIncludeTypeMap(f)
			if err != nil {
				return err
			}
		case Annotation:
			s.Annotations, err = parseToIncludeTypeMap(f)
			if err != nil {
				return err
			}
		case Container:
			s.Containers = parseToIncludeTypeSlice(f)
		}
	}

	return nil
}

func (s *SelectionSpec) MarshalJSON() ([]byte, error) {
	out := fmt.Sprint(strings.Join(s.Namespaces, ","))
	out += fmt.Sprintf("/%s", strings.Join(s.Deployments, ","))
	out += fmt.Sprintf("/%s", strings.Join(s.Pods, ","))
	tmp := []string{}
	for k, v := range s.Labels {
		tmp = append(tmp, fmt.Sprintf("%s=%s", k, v))
	}
	out += fmt.Sprintf("/%s", strings.Join(tmp, ","))
	tmp = []string{}
	for k, v := range s.Annotations {
		tmp = append(tmp, fmt.Sprintf("%s=%s", k, v))
	}
	out += fmt.Sprintf("/%s", strings.Join(tmp, ","))
	out += fmt.Sprintf("/%s", strings.Join(s.Containers, ","))
	return []byte(`"` + out + `"`), nil
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
