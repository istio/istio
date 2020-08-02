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
	"time"
)

// SelectionSpec is a spec for pods that will be included in the capture
// archive. The format is:
//
//   Namespace1,Namespace2../DeploymentName/PodName/ContainerName1,ContainerName2.../Label1,Label2.../Annotation1,Annotation2...
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
// size and logs must be excluded.
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
	Namespaces     map[IncludeType][]string
	DeploymentName string
	PodName        string
	ContainerNames map[IncludeType][]string
	Labels         map[IncludeType]map[string]string
	Annotations    map[IncludeType]map[string]string
}

// IncludeType selects whether an item included or excluded.
type IncludeType int

const (
	Include IncludeType = iota
	Exclude
)

// kubeCaptureConfig controls what is captured and included in the kube-capture tool
// archive.
type kubeCaptureConfig struct {
	// kubeConfigPath is the path to kube config file.
	kubeConfigPath string
	// context is the cluster context in the kube config
	context string

	// istioNamespaces is the list of namespaces where istio control planes
	// are installed.
	istioNamespaces []string

	// inFilename is a path to a config file.
	inFilename string

	// dryRun controls whether logs are actually captured and saved.
	dryRun bool
	// strict determines whether selection specs must match
	// at least one resource in the cluster. Used to check for typos in
	// cases where all selection names are expected to match at least
	// one resource.
	strict bool

	// commandTimeout is the maximum amount of time running the command
	// before giving up, even if not all logs are captured. Upon timeout,
	// the command creates an archive with only the logs captured so far.
	commandTimeout time.Duration
	// maxArchiveSizeMb is the maximum size of the archive in Mb. When
	// the maximum size is reached, logs are selectively discarded based
	// on importance metrics.
	maxArchiveSizeMb int32

	// included is a list of SelectionSpec entries for included pods.
	included []*SelectionSpec
	// excluded is a list of SelectionSpec entries for excluded pods.
	excluded []*SelectionSpec

	// startTime is the start time the the log capture time range.
	// If set, since must be unset.
	startTime time.Time
	// endTime is the end time the the log capture time range.
	// Default is now.
	endTime time.Time
	// since defines the start time the the log capture time range.
	// startTime is set to endTime - since.
	// If set, startTime must be unset.
	since time.Duration

	// criticalErrors is a list of glob pattern matches for errors that,
	// if found in a log, set the highest priority for the log to ensure
	// that it is included in the capture archive.
	criticalErrors []string
	// whitelistedErrors are glob error patterns which are ignored when
	// calculating the error heuristic for a log.
	whitelistedErrors []string

	// gcsURL is an URL to the GCS bucket where the archive is uploaded.
	gcsURL string
	// uploadToGCS uploads the archive to a GCS bucket. If no bucket
	// address is given, it creates one.
	uploadToGCS bool
}

var (
	includeTypeToStr = map[IncludeType]string{
		Include: "included",
		Exclude: "excluded",
	}
)
