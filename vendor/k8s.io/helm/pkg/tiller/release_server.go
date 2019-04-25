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

package tiller

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/technosophos/moniker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/hooks"
	"k8s.io/helm/pkg/proto/hapi/chart"
	"k8s.io/helm/pkg/proto/hapi/release"
	"k8s.io/helm/pkg/proto/hapi/services"
	relutil "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/tiller/environment"
	"k8s.io/helm/pkg/timeconv"
	"k8s.io/helm/pkg/version"
)

// releaseNameMaxLen is the maximum length of a release name.
//
// As of Kubernetes 1.4, the max limit on a name is 63 chars. We reserve 10 for
// charts to add data. Effectively, that gives us 53 chars.
// See https://github.com/kubernetes/helm/issues/1528
const releaseNameMaxLen = 53

// NOTESFILE_SUFFIX that we want to treat special. It goes through the templating engine
// but it's not a yaml file (resource) hence can't have hooks, etc. And the user actually
// wants to see this file after rendering in the status command. However, it must be a suffix
// since there can be filepath in front of it.
const notesFileSuffix = "NOTES.txt"

var (
	// errMissingChart indicates that a chart was not provided.
	errMissingChart = errors.New("no chart provided")
	// errMissingRelease indicates that a release (name) was not provided.
	errMissingRelease = errors.New("no release provided")
	// errInvalidRevision indicates that an invalid release revision number was provided.
	errInvalidRevision = errors.New("invalid release revision")
	//errInvalidName indicates that an invalid release name was provided
	errInvalidName = errors.New("invalid release name, must match regex ^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])+$ and the length must not longer than 53")
)

// ListDefaultLimit is the default limit for number of items returned in a list.
var ListDefaultLimit int64 = 512

// ValidName is a regular expression for names.
//
// According to the Kubernetes help text, the regular expression it uses is:
//
//	(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?
//
// We modified that. First, we added start and end delimiters. Second, we changed
// the final ? to + to require that the pattern match at least once. This modification
// prevents an empty string from matching.
var ValidName = regexp.MustCompile("^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])+$")

// ReleaseServer implements the server-side gRPC endpoint for the HAPI services.
type ReleaseServer struct {
	ReleaseModule
	env       *environment.Environment
	clientset kubernetes.Interface
	Log       func(string, ...interface{})
}

// NewReleaseServer creates a new release server.
func NewReleaseServer(env *environment.Environment, clientset kubernetes.Interface, useRemote bool) *ReleaseServer {
	var releaseModule ReleaseModule
	if useRemote {
		releaseModule = &RemoteReleaseModule{}
	} else {
		releaseModule = &LocalReleaseModule{
			clientset: clientset,
		}
	}

	return &ReleaseServer{
		env:           env,
		clientset:     clientset,
		ReleaseModule: releaseModule,
		Log:           func(_ string, _ ...interface{}) {},
	}
}

// reuseValues copies values from the current release to a new release if the
// new release does not have any values.
//
// If the request already has values, or if there are no values in the current
// release, this does nothing.
//
// This is skipped if the req.ResetValues flag is set, in which case the
// request values are not altered.
func (s *ReleaseServer) reuseValues(req *services.UpdateReleaseRequest, current *release.Release) error {
	if req.ResetValues {
		// If ResetValues is set, we comletely ignore current.Config.
		s.Log("resetting values to the chart's original version")
		return nil
	}

	// If the ReuseValues flag is set, we always copy the old values over the new config's values.
	if req.ReuseValues {
		s.Log("reusing the old release's values")

		// We have to regenerate the old coalesced values:
		oldVals, err := chartutil.CoalesceValues(current.Chart, current.Config)
		if err != nil {
			err := fmt.Errorf("failed to rebuild old values: %s", err)
			s.Log("%s", err)
			return err
		}
		nv, err := oldVals.YAML()
		if err != nil {
			return err
		}
		req.Chart.Values = &chart.Config{Raw: nv}

		reqValues, err := chartutil.ReadValues([]byte(req.Values.Raw))
		if err != nil {
			return err
		}

		currentConfig := chartutil.Values{}
		if current.Config != nil && current.Config.Raw != "" && current.Config.Raw != "{}\n" {
			currentConfig, err = chartutil.ReadValues([]byte(current.Config.Raw))
			if err != nil {
				return err
			}
		}

		currentConfig.MergeInto(reqValues)
		data, err := currentConfig.YAML()
		if err != nil {
			return err
		}

		req.Values.Raw = data
		return nil
	}

	// If req.Values is empty, but current.Config is not, copy current into the
	// request.
	if (req.Values == nil || req.Values.Raw == "" || req.Values.Raw == "{}\n") &&
		current.Config != nil &&
		current.Config.Raw != "" &&
		current.Config.Raw != "{}\n" {
		s.Log("copying values from %s (v%d) to new release.", current.Name, current.Version)
		req.Values = current.Config
	}
	return nil
}

func (s *ReleaseServer) uniqName(start string, reuse bool) (string, error) {

	// If a name is supplied, we check to see if that name is taken. If not, it
	// is granted. If reuse is true and a deleted release with that name exists,
	// we re-grant it. Otherwise, an error is returned.
	if start != "" {

		if len(start) > releaseNameMaxLen {
			return "", fmt.Errorf("release name %q exceeds max length of %d", start, releaseNameMaxLen)
		}

		h, err := s.env.Releases.History(start)
		if err != nil || len(h) < 1 {
			return start, nil
		}
		relutil.Reverse(h, relutil.SortByRevision)
		rel := h[0]

		if st := rel.Info.Status.Code; reuse && (st == release.Status_DELETED || st == release.Status_FAILED) {
			// Allowe re-use of names if the previous release is marked deleted.
			s.Log("name %s exists but is not in use, reusing name", start)
			return start, nil
		} else if reuse {
			return "", fmt.Errorf("a released named %s is in use, cannot re-use a name that is still in use", start)
		}

		return "", fmt.Errorf("a release named %s already exists.\nRun: helm ls --all %s; to check the status of the release\nOr run: helm del --purge %s; to delete it", start, start, start)
	}

	moniker := moniker.New()
	newname, err := s.createUniqName(moniker)
	if err != nil {
		return "ERROR", err
	}

	s.Log("info: Created new release name %s", newname)
	return newname, nil

}

func (s *ReleaseServer) createUniqName(m moniker.Namer) (string, error) {
	maxTries := 5
	for i := 0; i < maxTries; i++ {
		name := m.NameSep("-")
		if len(name) > releaseNameMaxLen {
			name = name[:releaseNameMaxLen]
		}
		if _, err := s.env.Releases.Get(name, 1); err != nil {
			if strings.Contains(err.Error(), "not found") {
				return name, nil
			}
		}
		s.Log("info: generated name %s is taken. Searching again.", name)
	}
	s.Log("warning: No available release names found after %d tries", maxTries)
	return "ERROR", errors.New("no available release name found")
}

func (s *ReleaseServer) engine(ch *chart.Chart) environment.Engine {
	renderer := s.env.EngineYard.Default()
	if ch.Metadata.Engine != "" {
		if r, ok := s.env.EngineYard.Get(ch.Metadata.Engine); ok {
			renderer = r
		} else {
			s.Log("warning: %s requested non-existent template engine %s", ch.Metadata.Name, ch.Metadata.Engine)
		}
	}
	return renderer
}

// capabilities builds a Capabilities from discovery information.
func capabilities(disc discovery.DiscoveryInterface) (*chartutil.Capabilities, error) {
	sv, err := disc.ServerVersion()
	if err != nil {
		return nil, err
	}
	vs, err := GetVersionSet(disc)
	if err != nil {
		return nil, fmt.Errorf("Could not get apiVersions from Kubernetes: %s", err)
	}
	return &chartutil.Capabilities{
		APIVersions:   vs,
		KubeVersion:   sv,
		TillerVersion: version.GetVersionProto(),
	}, nil
}

// GetVersionSet retrieves a set of available k8s API versions
func GetVersionSet(client discovery.ServerGroupsInterface) (chartutil.VersionSet, error) {
	groups, err := client.ServerGroups()
	if err != nil {
		return chartutil.DefaultVersionSet, err
	}

	// FIXME: The Kubernetes test fixture for cli appears to always return nil
	// for calls to Discovery().ServerGroups(). So in this case, we return
	// the default API list. This is also a safe value to return in any other
	// odd-ball case.
	if groups.Size() == 0 {
		return chartutil.DefaultVersionSet, nil
	}

	versions := metav1.ExtractGroupVersions(groups)
	return chartutil.NewVersionSet(versions...), nil
}

func (s *ReleaseServer) renderResources(ch *chart.Chart, values chartutil.Values, subNotes bool, vs chartutil.VersionSet) ([]*release.Hook, *bytes.Buffer, string, error) {
	// Guard to make sure Tiller is at the right version to handle this chart.
	sver := version.GetVersion()
	if ch.Metadata.TillerVersion != "" &&
		!version.IsCompatibleRange(ch.Metadata.TillerVersion, sver) {
		return nil, nil, "", fmt.Errorf("Chart incompatible with Tiller %s", sver)
	}

	if ch.Metadata.KubeVersion != "" {
		cap, _ := values["Capabilities"].(*chartutil.Capabilities)
		gitVersion := cap.KubeVersion.String()
		k8sVersion := strings.Split(gitVersion, "+")[0]
		if !version.IsCompatibleRange(ch.Metadata.KubeVersion, k8sVersion) {
			return nil, nil, "", fmt.Errorf("Chart requires kubernetesVersion: %s which is incompatible with Kubernetes %s", ch.Metadata.KubeVersion, k8sVersion)
		}
	}

	s.Log("rendering %s chart using values", ch.GetMetadata().Name)
	renderer := s.engine(ch)
	files, err := renderer.Render(ch, values)
	if err != nil {
		return nil, nil, "", err
	}

	// NOTES.txt gets rendered like all the other files, but because it's not a hook nor a resource,
	// pull it out of here into a separate file so that we can actually use the output of the rendered
	// text file. We have to spin through this map because the file contains path information, so we
	// look for terminating NOTES.txt. We also remove it from the files so that we don't have to skip
	// it in the sortHooks.
	var notesBuffer bytes.Buffer
	for k, v := range files {
		if strings.HasSuffix(k, notesFileSuffix) {
			if subNotes || (k == path.Join(ch.Metadata.Name, "templates", notesFileSuffix)) {

				// If buffer contains data, add newline before adding more
				if notesBuffer.Len() > 0 {
					notesBuffer.WriteString("\n")
				}
				notesBuffer.WriteString(v)
			}
			delete(files, k)
		}
	}

	notes := notesBuffer.String()

	// Sort hooks, manifests, and partials. Only hooks and manifests are returned,
	// as partials are not used after renderer.Render. Empty manifests are also
	// removed here.
	hooks, manifests, err := sortManifests(files, vs, InstallOrder)
	if err != nil {
		// By catching parse errors here, we can prevent bogus releases from going
		// to Kubernetes.
		//
		// We return the files as a big blob of data to help the user debug parser
		// errors.
		b := bytes.NewBuffer(nil)
		for name, content := range files {
			if len(strings.TrimSpace(content)) == 0 {
				continue
			}
			b.WriteString("\n---\n# Source: " + name + "\n")
			b.WriteString(content)
		}
		return nil, b, "", err
	}

	// Aggregate all valid manifests into one big doc.
	b := bytes.NewBuffer(nil)
	for _, m := range manifests {
		b.WriteString("\n---\n# Source: " + m.Name + "\n")
		b.WriteString(m.Content)
	}

	return hooks, b, notes, nil
}

// recordRelease with an update operation in case reuse has been set.
func (s *ReleaseServer) recordRelease(r *release.Release, reuse bool) {
	if reuse {
		if err := s.env.Releases.Update(r); err != nil {
			s.Log("warning: Failed to update release %s: %s", r.Name, err)
		}
	} else if err := s.env.Releases.Create(r); err != nil {
		s.Log("warning: Failed to record release %s: %s", r.Name, err)
	}
}

func (s *ReleaseServer) execHook(hs []*release.Hook, name, namespace, hook string, timeout int64) error {
	kubeCli := s.env.KubeClient
	code, ok := events[hook]
	if !ok {
		return fmt.Errorf("unknown hook %s", hook)
	}

	s.Log("executing %d %s hooks for %s", len(hs), hook, name)
	executingHooks := []*release.Hook{}
	for _, h := range hs {
		for _, e := range h.Events {
			if e == code {
				executingHooks = append(executingHooks, h)
			}
		}
	}

	executingHooks = sortByHookWeight(executingHooks)

	for _, h := range executingHooks {
		if err := s.deleteHookByPolicy(h, hooks.BeforeHookCreation, name, namespace, hook, kubeCli); err != nil {
			return err
		}

		b := bytes.NewBufferString(h.Manifest)
		if err := kubeCli.Create(namespace, b, timeout, false); err != nil {
			s.Log("warning: Release %s %s %s failed: %s", name, hook, h.Path, err)
			return err
		}
		// No way to rewind a bytes.Buffer()?
		b.Reset()
		b.WriteString(h.Manifest)

		// We can't watch CRDs
		if hook != hooks.CRDInstall {
			if err := kubeCli.WatchUntilReady(namespace, b, timeout, false); err != nil {
				s.Log("warning: Release %s %s %s could not complete: %s", name, hook, h.Path, err)
				// If a hook is failed, checkout the annotation of the hook to determine whether the hook should be deleted
				// under failed condition. If so, then clear the corresponding resource object in the hook
				if err := s.deleteHookByPolicy(h, hooks.HookFailed, name, namespace, hook, kubeCli); err != nil {
					return err
				}
				return err
			}
		}
	}

	s.Log("hooks complete for %s %s", hook, name)
	// If all hooks are succeeded, checkout the annotation of each hook to determine whether the hook should be deleted
	// under succeeded condition. If so, then clear the corresponding resource object in each hook
	for _, h := range executingHooks {
		if err := s.deleteHookByPolicy(h, hooks.HookSucceeded, name, namespace, hook, kubeCli); err != nil {
			return err
		}
		h.LastRun = timeconv.Now()
	}

	return nil
}

func validateManifest(c environment.KubeClient, ns string, manifest []byte) error {
	r := bytes.NewReader(manifest)
	_, err := c.BuildUnstructured(ns, r)
	return err
}

func validateReleaseName(releaseName string) error {
	if releaseName == "" {
		return errMissingRelease
	}

	if !ValidName.MatchString(releaseName) || (len(releaseName) > releaseNameMaxLen) {
		return errInvalidName
	}

	return nil
}

func (s *ReleaseServer) deleteHookByPolicy(h *release.Hook, policy string, name, namespace, hook string, kubeCli environment.KubeClient) error {
	b := bytes.NewBufferString(h.Manifest)
	if hookHasDeletePolicy(h, policy) {
		s.Log("deleting %s hook %s for release %s due to %q policy", hook, h.Name, name, policy)
		if errHookDelete := kubeCli.Delete(namespace, b); errHookDelete != nil {
			s.Log("warning: Release %s %s %S could not be deleted: %s", name, hook, h.Path, errHookDelete)
			return errHookDelete
		}
	}
	return nil
}

// hookHasDeletePolicy determines whether the defined hook deletion policy matches the hook deletion polices
// supported by helm. If so, mark the hook as one should be deleted.
func hookHasDeletePolicy(h *release.Hook, policy string) bool {
	if dp, ok := deletePolices[policy]; ok {
		for _, v := range h.DeletePolicies {
			if dp == v {
				return true
			}
		}
	}
	return false
}
