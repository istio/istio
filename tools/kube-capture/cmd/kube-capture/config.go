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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	cluster "istio.io/istio/tools/kube-capture/pkg"
)

// SelectionSpec is a spec for pods that will be Included in the capture
// archive. The format is:
//
//   Namespace1,Namespace2../Deployments/Pods/ContainerName1,ContainerName2.../Label1,Label2.../Annotation1,Annotation2...
//
// Namespace, pod and container names are pattern matching while labels
// and annotations may have pattern in the values with exact match for keys.
// All labels and annotations in the list must match, unless the label/annotation
// is preceded by -, in which case the pod must NOT have the label.
// Label/annotation values are optional, if they are not specified, all values
// match.
// Pattern matching style is glob.
// Exclusions have a higher precedence than inclusions.
// Ordering defines pod priority for cases where the archive exceeds the maximum
// size and logs must be Excluded.
//
// Examples:
//
// 1. All pods in test-namespace with label "test" but without label "private":
//      test-namespace/*/*/test,-private/*
//
// 2. Pods in all namespaces except "kube-system" with annotation "revision"
// matching wildcard 1.6*:
//      -kube-system/*/*/*/revision=1.6*
//
// 3. Pods in all namespaces with "prometheus" in the name, except those with
// the annotation "internal=true":
//      */*prometheus*/*/*/-internal=true
//
// 4. Container logs for all containers called "istio-proxy":
//      */*/istio-proxy/*
//
type SelectionSpec struct {
	Namespaces  map[IncludeType][]string
	Deployments map[IncludeType][]string
	Pods        map[IncludeType][]string
	Containers  map[IncludeType][]string
	Labels      map[IncludeType]map[string]string
	Annotations map[IncludeType]map[string]string
}

// IncludeType selects whether an item Included or Excluded.
type IncludeType int

const (
	Include IncludeType = iota
	Exclude
)

// KubeCaptureConfig controls what is captured and Included in the kube-capture tool
// archive.
type KubeCaptureConfig struct {
	// KubeConfigPath is the path to kube config file.
	KubeConfigPath string `json:"kubeConfigPath"`
	// Context is the cluster Context in the kube config
	Context string `json:"context"`

	// IstioNamespaces is the list of namespaces where istio control planes
	// are installed.
	IstioNamespaces []string `json:"istioNamespaces"`

	// DryRun controls whether logs are actually captured and saved.
	DryRun bool `json:"dryRun"`
	// Strict determines whether selection specs must match
	// at least one resource in the cluster. Used to check for typos in
	// cases where all selection names are expected to match at least
	// one resource.
	Strict bool `json:"strict"`

	// CommandTimeout is the maximum amount of time running the command
	// before giving up, even if not all logs are captured. Upon timeout,
	// the command creates an archive with only the logs captured so far.
	CommandTimeout Duration `json:"commandTimeout"`
	// MaxArchiveSizeMb is the maximum size of the archive in Mb. When
	// the maximum size is reached, logs are selectively discarded based
	// on importance metrics.
	MaxArchiveSizeMb int32 `json:"maxArchiveSizeMb"`

	// Included is a list of SelectionSpec entries for Included pods.
	Included []*SelectionSpec `json:"included"`
	// Excluded is a list of SelectionSpec entries for Excluded pods.
	Excluded []*SelectionSpec `json:"excluded"`

	// StartTime is the start time the the log capture time range.
	// If set, Since must be unset.
	StartTime time.Time `json:"startTime"`
	// EndTime is the end time the the log capture time range.
	// Default is now.
	EndTime time.Time `json:"endTime"`
	// Since defines the start time the the log capture time range.
	// StartTime is set to EndTime - Since.
	// If set, StartTime must be unset.
	Since Duration `json:"since"`

	// CriticalErrors is a list of glob pattern matches for errors that,
	// if found in a log, set the highest priority for the log to ensure
	// that it is Included in the capture archive.
	CriticalErrors []string `json:"criticalErrors"`
	// WhitelistedErrors are glob error patterns which are ignored when
	// calculating the error heuristic for a log.
	WhitelistedErrors []string `json:"whitelistedErrors"`

	// GCSURL is an URL to the GCS bucket where the archive is uploaded.
	GCSURL string `json:"gcsURL"`
	// UploadToGCS uploads the archive to a GCS bucket. If no bucket
	// address is given, it creates one.
	UploadToGCS bool `json:"uploadToGCS"`
}

var (
	includeTypeToStr = map[IncludeType]string{
		Include: "Included",
		Exclude: "Excluded",
	}
)

func parseToIncludeTypeSlice(s string) (map[IncludeType][]string, error) {
	out := make(map[IncludeType][]string)
	for _, ss := range strings.Split(s, ",") {
		if len(ss) == 0 {
			continue
		}
		if err := validateK8sName(ss); err != nil {
			return nil, err
		}
		v := ss[0:]
		t := Include
		if ss[0] == '-' {
			v = ss[1:]
			t = Exclude
		}
		out[t] = append(out[t], v)
	}
	return out, nil
}

func parseToIncludeTypeMap(s string) (map[IncludeType]map[string]string, error) {
	out := map[IncludeType]map[string]string{
		Include: make(map[string]string),
		Exclude: make(map[string]string),
	}
	for _, ss := range strings.Split(s, ",") {
		if len(ss) == 0 {
			continue
		}
		v := ss[0:]
		t := Include
		if ss[0] == '-' {
			v = ss[1:]
			t = Exclude
		}
		kv := strings.Split(v, "=")
		if len(kv) != 2 {
			return nil, fmt.Errorf("bad label/annotation selection %s, must have format key=value", v)
		}
		out[t][kv[0]] = kv[1]
	}
	return out, nil
}

func (s *SelectionSpec) UnmarshalJSON(b []byte) error {
	ft := []cluster.ResourceType{cluster.Namespace, cluster.Deployment, cluster.Pod, cluster.Label, cluster.Annotation, cluster.Container}
	str := strings.TrimPrefix(strings.TrimSuffix(string(b), `"`), `"`)
	for i, f := range strings.Split(str, "/") {
		var err error
		switch ft[i] {
		case cluster.Namespace:
			s.Namespaces, err = parseToIncludeTypeSlice(f)
			if err != nil {
				return err
			}
		case cluster.Deployment:
			s.Deployments, err = parseToIncludeTypeSlice(f)
			if err != nil {
				return err
			}
		case cluster.Pod:
			s.Pods, err = parseToIncludeTypeSlice(f)
			if err != nil {
				return err
			}
		case cluster.Label:
			s.Labels, err = parseToIncludeTypeMap(f)
			if err != nil {
				return err
			}
		case cluster.Annotation:
			s.Annotations, err = parseToIncludeTypeMap(f)
			if err != nil {
				return err
			}
		case cluster.Container:
			s.Containers, err = parseToIncludeTypeSlice(f)
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
