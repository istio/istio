/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*Package environment describes the operating environment for Tiller.

Tiller's environment encapsulates all of the service dependencies Tiller has.
These dependencies are expressed as interfaces so that alternate implementations
(mocks, etc.) can be easily generated.
*/
package environment

import (
	"io"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions/resource"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/engine"
	"k8s.io/helm/pkg/kube"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/storage"
	"k8s.io/helm/pkg/storage/driver"
)

// DefaultTillerNamespace is the default namespace for Tiller.
const DefaultTillerNamespace = "kube-system"

// GoTplEngine is the name of the Go template engine, as registered in the EngineYard.
const GoTplEngine = "gotpl"

// DefaultEngine points to the engine that the EngineYard should treat as the
// default. A chart that does not specify an engine may be run through the
// default engine.
var DefaultEngine = GoTplEngine

// EngineYard maps engine names to engine implementations.
type EngineYard map[string]Engine

// Get retrieves a template engine by name.
//
// If no matching template engine is found, the second return value will
// be false.
func (y EngineYard) Get(k string) (Engine, bool) {
	e, ok := y[k]
	return e, ok
}

// Default returns the default template engine.
//
// The default is specified by DefaultEngine.
//
// If the default template engine cannot be found, this panics.
func (y EngineYard) Default() Engine {
	d, ok := y[DefaultEngine]
	if !ok {
		// This is a developer error!
		panic("Default template engine does not exist")
	}
	return d
}

// Engine represents a template engine that can render templates.
//
// For some engines, "rendering" includes both compiling and executing. (Other
// engines do not distinguish between phases.)
//
// The engine returns a map where the key is the named output entity (usually
// a file name) and the value is the rendered content of the template.
//
// An Engine must be capable of executing multiple concurrent requests, but
// without tainting one request's environment with data from another request.
type Engine interface {
	// Render renders a chart.
	//
	// It receives a chart, a config, and a map of overrides to the config.
	// Overrides are assumed to be passed from the system, not the user.
	Render(*chart.Chart, chartutil.Values) (map[string]string, error)
}

// KubeClient represents a client capable of communicating with the Kubernetes API.
//
// A KubeClient must be concurrency safe.
type KubeClient interface {
	// Create creates one or more resources.
	//
	// reader must contain a YAML stream (one or more YAML documents separated
	// by "\n---\n").
	Create(namespace string, reader io.Reader, timeout int64, shouldWait bool) error

	// Get gets one or more resources. Returned string hsa the format like kubectl
	// provides with the column headers separating the resource types.
	//
	// namespace must contain a valid existing namespace.
	//
	// reader must contain a YAML stream (one or more YAML documents separated
	// by "\n---\n").
	Get(namespace string, reader io.Reader) (string, error)

	// Delete destroys one or more resources.
	//
	// namespace must contain a valid existing namespace.
	//
	// reader must contain a YAML stream (one or more YAML documents separated
	// by "\n---\n").
	Delete(namespace string, reader io.Reader) error

	// WatchUntilReady watch the resource in reader until it is "ready".
	//
	// For Jobs, "ready" means the job ran to completion (excited without error).
	// For all other kinds, it means the kind was created or modified without
	// error.
	WatchUntilReady(namespace string, reader io.Reader, timeout int64, shouldWait bool) error

	// Update updates one or more resources or creates the resource
	// if it doesn't exist.
	//
	// namespace must contain a valid existing namespace.
	//
	// reader must contain a YAML stream (one or more YAML documents separated
	// by "\n---\n").
	Update(namespace string, originalReader, modifiedReader io.Reader, force bool, recreate bool, timeout int64, shouldWait bool) error

	Build(namespace string, reader io.Reader) (kube.Result, error)
	BuildUnstructured(namespace string, reader io.Reader) (kube.Result, error)

	// WaitAndGetCompletedPodPhase waits up to a timeout until a pod enters a completed phase
	// and returns said phase (PodSucceeded or PodFailed qualify).
	WaitAndGetCompletedPodPhase(namespace string, reader io.Reader, timeout time.Duration) (v1.PodPhase, error)
}

// PrintingKubeClient implements KubeClient, but simply prints the reader to
// the given output.
type PrintingKubeClient struct {
	Out io.Writer
}

// Create prints the values of what would be created with a real KubeClient.
func (p *PrintingKubeClient) Create(ns string, r io.Reader, timeout int64, shouldWait bool) error {
	_, err := io.Copy(p.Out, r)
	return err
}

// Get prints the values of what would be created with a real KubeClient.
func (p *PrintingKubeClient) Get(ns string, r io.Reader) (string, error) {
	_, err := io.Copy(p.Out, r)
	return "", err
}

// Delete implements KubeClient delete.
//
// It only prints out the content to be deleted.
func (p *PrintingKubeClient) Delete(ns string, r io.Reader) error {
	_, err := io.Copy(p.Out, r)
	return err
}

// WatchUntilReady implements KubeClient WatchUntilReady.
func (p *PrintingKubeClient) WatchUntilReady(ns string, r io.Reader, timeout int64, shouldWait bool) error {
	_, err := io.Copy(p.Out, r)
	return err
}

// Update implements KubeClient Update.
func (p *PrintingKubeClient) Update(ns string, currentReader, modifiedReader io.Reader, force bool, recreate bool, timeout int64, shouldWait bool) error {
	_, err := io.Copy(p.Out, modifiedReader)
	return err
}

// Build implements KubeClient Build.
func (p *PrintingKubeClient) Build(ns string, reader io.Reader) (kube.Result, error) {
	return []*resource.Info{}, nil
}

// BuildUnstructured implements KubeClient BuildUnstructured.
func (p *PrintingKubeClient) BuildUnstructured(ns string, reader io.Reader) (kube.Result, error) {
	return []*resource.Info{}, nil
}

// WaitAndGetCompletedPodPhase implements KubeClient WaitAndGetCompletedPodPhase.
func (p *PrintingKubeClient) WaitAndGetCompletedPodPhase(namespace string, reader io.Reader, timeout time.Duration) (v1.PodPhase, error) {
	_, err := io.Copy(p.Out, reader)
	return v1.PodUnknown, err
}

// Environment provides the context for executing a client request.
//
// All services in a context are concurrency safe.
type Environment struct {
	// EngineYard provides access to the known template engines.
	EngineYard EngineYard
	// Releases stores records of releases.
	Releases *storage.Storage
	// KubeClient is a Kubernetes API client.
	KubeClient KubeClient
}

// New returns an environment initialized with the defaults.
func New() *Environment {
	e := engine.New()
	var ey EngineYard = map[string]Engine{
		// Currently, the only template engine we support is the GoTpl one. But
		// we can easily add some here.
		GoTplEngine: e,
	}

	return &Environment{
		EngineYard: ey,
		Releases:   storage.Init(driver.NewMemory()),
	}
}
