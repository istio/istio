// Copyright 2018 The Operator-SDK Authors
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

package runner

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/operator-framework/operator-sdk/pkg/ansible/paramconv"
	"github.com/operator-framework/operator-sdk/pkg/ansible/runner/eventapi"
	"github.com/operator-framework/operator-sdk/pkg/ansible/runner/internal/inputdir"

	yaml "gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("runner")

const (
	// MaxRunnerArtifactsAnnotation - annotation used by a user to specify the max artifacts to keep
	// in the runner directory. This will override the value provided by the watches file for a
	// particular CR. Setting this to zero will cause all artifact directories to be kept.
	// Example usage "ansible.operator-sdk/max-runner-artifacts: 100"
	MaxRunnerArtifactsAnnotation = "ansible.operator-sdk/max-runner-artifacts"
)

// Runner - a runnable that should take the parameters and name and namespace
// and run the correct code.
type Runner interface {
	Run(string, *unstructured.Unstructured, string) (RunResult, error)
	GetFinalizer() (string, bool)
	GetReconcilePeriod() (time.Duration, bool)
	GetManageStatus() bool
	GetWatchDependentResources() bool
	GetWatchClusterScopedResources() bool
}

// watch holds data used to create a mapping of GVK to ansible playbook or role.
// The mapping is used to compose an ansible operator.
type watch struct {
	MaxRunnerArtifacts          int        `yaml:"maxRunnerArtifacts"`
	Version                     string     `yaml:"version"`
	Group                       string     `yaml:"group"`
	Kind                        string     `yaml:"kind"`
	Playbook                    string     `yaml:"playbook"`
	Role                        string     `yaml:"role"`
	ReconcilePeriod             string     `yaml:"reconcilePeriod"`
	ManageStatus                bool       `yaml:"manageStatus"`
	WatchDependentResources     bool       `yaml:"watchDependentResources"`
	WatchClusterScopedResources bool       `yaml:"watchClusterScopedResources"`
	Finalizer                   *Finalizer `yaml:"finalizer"`
}

// Finalizer - Expose finalizer to be used by a user.
type Finalizer struct {
	Name     string                 `yaml:"name"`
	Playbook string                 `yaml:"playbook"`
	Role     string                 `yaml:"role"`
	Vars     map[string]interface{} `yaml:"vars"`
}

// UnmarshalYaml - implements the yaml.Unmarshaler interface
func (w *watch) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// by default, the operator will manage status and watch dependent resources
	// The operator will not manage cluster scoped resources by default.
	w.ManageStatus = true
	w.WatchDependentResources = true
	w.MaxRunnerArtifacts = 20
	w.WatchClusterScopedResources = false

	// hide watch data in plain struct to prevent unmarshal from calling
	// UnmarshalYAML again
	type plain watch

	return unmarshal((*plain)(w))
}

// NewFromWatches reads the operator's config file at the provided path.
func NewFromWatches(path string) (map[schema.GroupVersionKind]Runner, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		log.Error(err, "Failed to get config file")
		return nil, err
	}
	watches := []watch{}
	err = yaml.Unmarshal(b, &watches)
	if err != nil {
		log.Error(err, "Failed to unmarshal config")
		return nil, err
	}

	m := map[schema.GroupVersionKind]Runner{}
	for _, w := range watches {
		s := schema.GroupVersionKind{
			Group:   w.Group,
			Version: w.Version,
			Kind:    w.Kind,
		}
		var reconcilePeriod *time.Duration
		if w.ReconcilePeriod != "" {
			d, err := time.ParseDuration(w.ReconcilePeriod)
			if err != nil {
				return nil, fmt.Errorf("unable to parse duration: %v - %v, setting to default", w.ReconcilePeriod, err)
			}
			reconcilePeriod = &d
		}

		// Check if schema is a duplicate
		if _, ok := m[s]; ok {
			return nil, fmt.Errorf("duplicate GVK: %v", s.String())
		}
		switch {
		case w.Playbook != "":
			r, err := NewForPlaybook(w.Playbook, s, w.Finalizer, reconcilePeriod, w.ManageStatus, w.WatchDependentResources, w.WatchClusterScopedResources, w.MaxRunnerArtifacts)
			if err != nil {
				return nil, err
			}
			m[s] = r
		case w.Role != "":
			r, err := NewForRole(w.Role, s, w.Finalizer, reconcilePeriod, w.ManageStatus, w.WatchDependentResources, w.WatchClusterScopedResources, w.MaxRunnerArtifacts)
			if err != nil {
				return nil, err
			}
			m[s] = r
		default:
			return nil, fmt.Errorf("either playbook or role must be defined for %v", s)
		}
	}
	return m, nil
}

// NewForPlaybook returns a new Runner based on the path to an ansible playbook.
func NewForPlaybook(path string, gvk schema.GroupVersionKind, finalizer *Finalizer, reconcilePeriod *time.Duration, manageStatus, dependentResources, clusterScopedResources bool, maxArtifacts int) (Runner, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("playbook path must be absolute for %v", gvk)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("playbook: %v was not found for %v", path, gvk)
	}
	r := &runner{
		Path: path,
		GVK:  gvk,
		cmdFunc: func(ident, inputDirPath string, maxArtifacts int) *exec.Cmd {
			return exec.Command("ansible-runner", "-vv", "--rotate-artifacts", fmt.Sprintf("%v", maxArtifacts), "-p", path, "-i", ident, "run", inputDirPath)
		},
		maxRunnerArtifacts:          maxArtifacts,
		reconcilePeriod:             reconcilePeriod,
		manageStatus:                manageStatus,
		watchDependentResources:     dependentResources,
		watchClusterScopedResources: clusterScopedResources,
	}
	err := r.addFinalizer(finalizer)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// NewForRole returns a new Runner based on the path to an ansible role.
func NewForRole(path string, gvk schema.GroupVersionKind, finalizer *Finalizer, reconcilePeriod *time.Duration, manageStatus, dependentResources, clusterScopedResources bool, maxArtifacts int) (Runner, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("role path must be absolute for %v", gvk)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("role path: %v was not found for %v", path, gvk)
	}
	path = strings.TrimRight(path, "/")
	r := &runner{
		Path: path,
		GVK:  gvk,
		cmdFunc: func(ident, inputDirPath string, maxArtifacts int) *exec.Cmd {
			rolePath, roleName := filepath.Split(path)
			return exec.Command("ansible-runner", "-vv", "--rotate-artifacts", fmt.Sprintf("%v", maxArtifacts), "--role", roleName, "--roles-path", rolePath, "--hosts", "localhost", "-i", ident, "run", inputDirPath)
		},
		maxRunnerArtifacts:          maxArtifacts,
		reconcilePeriod:             reconcilePeriod,
		manageStatus:                manageStatus,
		watchDependentResources:     dependentResources,
		watchClusterScopedResources: clusterScopedResources,
	}
	err := r.addFinalizer(finalizer)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// runner - implements the Runner interface for a GVK that's being watched.
type runner struct {
	maxRunnerArtifacts          int
	Path                        string                  // path on disk to a playbook or role depending on what cmdFunc expects
	GVK                         schema.GroupVersionKind // GVK being watched that corresponds to the Path
	Finalizer                   *Finalizer
	cmdFunc                     func(ident, inputDirPath string, maxArtifacts int) *exec.Cmd // returns a Cmd that runs ansible-runner
	finalizerCmdFunc            func(ident, inputDirPath string, maxArtifacts int) *exec.Cmd
	reconcilePeriod             *time.Duration
	manageStatus                bool
	watchDependentResources     bool
	watchClusterScopedResources bool
}

func (r *runner) Run(ident string, u *unstructured.Unstructured, kubeconfig string) (RunResult, error) {
	if u.GetDeletionTimestamp() != nil && !r.isFinalizerRun(u) {
		return nil, errors.New("resource has been deleted, but no finalizer was matched, skipping reconciliation")
	}
	logger := log.WithValues(
		"job", ident,
		"name", u.GetName(),
		"namespace", u.GetNamespace(),
	)

	// start the event receiver. We'll check errChan for an error after
	// ansible-runner exits.
	errChan := make(chan error, 1)
	receiver, err := eventapi.New(ident, errChan)
	if err != nil {
		return nil, err
	}
	inputDir := inputdir.InputDir{
		Path:       filepath.Join("/tmp/ansible-operator/runner/", r.GVK.Group, r.GVK.Version, r.GVK.Kind, u.GetNamespace(), u.GetName()),
		Parameters: r.makeParameters(u),
		EnvVars: map[string]string{
			"K8S_AUTH_KUBECONFIG": kubeconfig,
			"KUBECONFIG":          kubeconfig,
		},
		Settings: map[string]string{
			"runner_http_url":  receiver.SocketPath,
			"runner_http_path": receiver.URLPath,
		},
	}
	// If Path is a dir, assume it is a role path. Otherwise assume it's a
	// playbook path
	fi, err := os.Lstat(r.Path)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		inputDir.PlaybookPath = r.Path
	}
	err = inputDir.Write()
	if err != nil {
		return nil, err
	}
	maxArtifacts := r.maxRunnerArtifacts
	if ma, ok := u.GetAnnotations()[MaxRunnerArtifactsAnnotation]; ok {
		i, err := strconv.Atoi(ma)
		if err != nil {
			log.Info("Invalid max runner artifact annotation", "err", err, "value", ma)
		}
		maxArtifacts = i
	}

	go func() {
		var dc *exec.Cmd
		if r.isFinalizerRun(u) {
			logger.V(1).Info("Resource is marked for deletion, running finalizer", "Finalizer", r.Finalizer.Name)
			dc = r.finalizerCmdFunc(ident, inputDir.Path, maxArtifacts)
		} else {
			dc = r.cmdFunc(ident, inputDir.Path, maxArtifacts)
		}
		// Append current environment since setting dc.Env to anything other than nil overwrites current env
		dc.Env = append(dc.Env, os.Environ()...)
		dc.Env = append(dc.Env, fmt.Sprintf("K8S_AUTH_KUBECONFIG=%s", kubeconfig), fmt.Sprintf("KUBECONFIG=%s", kubeconfig))

		output, err := dc.CombinedOutput()
		if err != nil {
			logger.Error(err, string(output))
		} else {
			logger.Info("Ansible-runner exited successfully")
		}

		receiver.Close()
		err = <-errChan
		// http.Server returns this in the case of being closed cleanly
		if err != nil && err != http.ErrServerClosed {
			logger.Error(err, "Error from event API")
		}

		// link the current run to the `latest` directory under artifacts
		currentRun := filepath.Join(inputDir.Path, "artifacts", ident)
		latestArtifacts := filepath.Join(inputDir.Path, "artifacts", "latest")
		if _, err = os.Lstat(latestArtifacts); err == nil {
			if err = os.Remove(latestArtifacts); err != nil {
				logger.Error(err, "Error removing the latest artifacts symlink")
			}
		}
		if err = os.Symlink(currentRun, latestArtifacts); err != nil {
			logger.Error(err, "Error symlinking latest artifacts")
		}

	}()

	return &runResult{
		events:   receiver.Events,
		inputDir: &inputDir,
		ident:    ident,
	}, nil
}

// GetReconcilePeriod - new reconcile period.
func (r *runner) GetReconcilePeriod() (time.Duration, bool) {
	if r.reconcilePeriod == nil {
		return time.Duration(0), false
	}
	return *r.reconcilePeriod, true
}

// GetManageStatus - get the manage status
func (r *runner) GetManageStatus() bool {
	return r.manageStatus
}

// GetWatchDependentResources - get the watch dependent resources value
func (r *runner) GetWatchDependentResources() bool {
	return r.watchDependentResources
}

// GetWatchClusterScopedResources - get the watch cluster scoped resources value
func (r *runner) GetWatchClusterScopedResources() bool {
	return r.watchClusterScopedResources
}

func (r *runner) GetFinalizer() (string, bool) {
	if r.Finalizer != nil {
		return r.Finalizer.Name, true
	}
	return "", false
}

func (r *runner) isFinalizerRun(u *unstructured.Unstructured) bool {
	finalizersSet := r.Finalizer != nil && u.GetFinalizers() != nil
	// The resource is deleted and our finalizer is present, we need to run the finalizer
	if finalizersSet && u.GetDeletionTimestamp() != nil {
		for _, f := range u.GetFinalizers() {
			if f == r.Finalizer.Name {
				return true
			}
		}
	}
	return false
}

func (r *runner) addFinalizer(finalizer *Finalizer) error {
	r.Finalizer = finalizer
	switch {
	case finalizer == nil:
		return nil
	case finalizer.Playbook != "":
		if !filepath.IsAbs(finalizer.Playbook) {
			return fmt.Errorf("finalizer playbook path must be absolute for %v", r.GVK)
		}
		r.finalizerCmdFunc = func(ident, inputDirPath string, maxArtifacts int) *exec.Cmd {
			return exec.Command("ansible-runner", "-vv", "--rotate-artifacts", fmt.Sprintf("%v", maxArtifacts), "-p", finalizer.Playbook, "-i", ident, "run", inputDirPath)
		}
	case finalizer.Role != "":
		if !filepath.IsAbs(finalizer.Role) {
			return fmt.Errorf("finalizer role path must be absolute for %v", r.GVK)
		}
		r.finalizerCmdFunc = func(ident, inputDirPath string, maxArtifacts int) *exec.Cmd {
			path := strings.TrimRight(finalizer.Role, "/")
			rolePath, roleName := filepath.Split(path)
			return exec.Command("ansible-runner", "-vv", "--rotate-artifacts", fmt.Sprintf("%v", maxArtifacts), "--role", roleName, "--roles-path", rolePath, "--hosts", "localhost", "-i", ident, "run", inputDirPath)
		}
	case len(finalizer.Vars) != 0:
		r.finalizerCmdFunc = r.cmdFunc
	}
	return nil
}

// makeParameters - creates the extravars parameters for ansible
// The resulting structure in json is:
// { "meta": {
//      "name": <object_name>,
//      "namespace": <object_namespace>,
//   },
//   <cr_spec_fields_as_snake_case>,
//   ...
//   _<group_as_snake>_<kind>: {
//       <cr_object as is
//   }
// }
func (r *runner) makeParameters(u *unstructured.Unstructured) map[string]interface{} {
	s := u.Object["spec"]
	spec, ok := s.(map[string]interface{})
	if !ok {
		log.Info("Spec was not found for CR", "GroupVersionKind", u.GroupVersionKind(), "Namespace", u.GetNamespace(), "Name", u.GetName())
		spec = map[string]interface{}{}
	}
	parameters := paramconv.MapToSnake(spec)
	parameters["meta"] = map[string]string{"namespace": u.GetNamespace(), "name": u.GetName()}
	objectKey := fmt.Sprintf("_%v_%v", strings.Replace(r.GVK.Group, ".", "_", -1), strings.ToLower(r.GVK.Kind))
	parameters[objectKey] = u.Object
	if r.isFinalizerRun(u) {
		for k, v := range r.Finalizer.Vars {
			parameters[k] = v
		}
	}
	return parameters
}

// RunResult - result of a ansible run
type RunResult interface {
	// Stdout returns the stdout from ansible-runner if it is available, else an error.
	Stdout() (string, error)
	// Events returns the events from ansible-runner if it is available, else an error.
	Events() <-chan eventapi.JobEvent
}

// RunResult facilitates access to information about a run of ansible.
type runResult struct {
	// Events is a channel of events from ansible that contain state related
	// to a run of ansible.
	events <-chan eventapi.JobEvent

	ident    string
	inputDir *inputdir.InputDir
}

// Stdout returns the stdout from ansible-runner if it is available, else an error.
func (r *runResult) Stdout() (string, error) {
	return r.inputDir.Stdout(r.ident)
}

// Events returns the events from ansible-runner if it is available, else an error.
func (r *runResult) Events() <-chan eventapi.JobEvent {
	return r.events
}
