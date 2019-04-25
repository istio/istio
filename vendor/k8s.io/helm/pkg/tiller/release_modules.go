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
	"fmt"
	"log"
	"strings"

	"k8s.io/client-go/kubernetes"

	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/kube"
	"k8s.io/helm/pkg/proto/hapi/release"
	rudderAPI "k8s.io/helm/pkg/proto/hapi/rudder"
	"k8s.io/helm/pkg/proto/hapi/services"
	relutil "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/rudder"
	"k8s.io/helm/pkg/tiller/environment"
)

// ReleaseModule is an interface that allows ReleaseServer to run operations on release via either local implementation or Rudder service
type ReleaseModule interface {
	Create(r *release.Release, req *services.InstallReleaseRequest, env *environment.Environment) error
	Update(current, target *release.Release, req *services.UpdateReleaseRequest, env *environment.Environment) error
	Rollback(current, target *release.Release, req *services.RollbackReleaseRequest, env *environment.Environment) error
	Status(r *release.Release, req *services.GetReleaseStatusRequest, env *environment.Environment) (string, error)
	Delete(r *release.Release, req *services.UninstallReleaseRequest, env *environment.Environment) (string, []error)
}

// LocalReleaseModule is a local implementation of ReleaseModule
type LocalReleaseModule struct {
	clientset kubernetes.Interface
}

// Create creates a release via kubeclient from provided environment
func (m *LocalReleaseModule) Create(r *release.Release, req *services.InstallReleaseRequest, env *environment.Environment) error {
	b := bytes.NewBufferString(r.Manifest)
	return env.KubeClient.Create(r.Namespace, b, req.Timeout, req.Wait)
}

// Update performs an update from current to target release
func (m *LocalReleaseModule) Update(current, target *release.Release, req *services.UpdateReleaseRequest, env *environment.Environment) error {
	c := bytes.NewBufferString(current.Manifest)
	t := bytes.NewBufferString(target.Manifest)
	return env.KubeClient.Update(target.Namespace, c, t, req.Force, req.Recreate, req.Timeout, req.Wait)
}

// Rollback performs a rollback from current to target release
func (m *LocalReleaseModule) Rollback(current, target *release.Release, req *services.RollbackReleaseRequest, env *environment.Environment) error {
	c := bytes.NewBufferString(current.Manifest)
	t := bytes.NewBufferString(target.Manifest)
	return env.KubeClient.Update(target.Namespace, c, t, req.Force, req.Recreate, req.Timeout, req.Wait)
}

// Status returns kubectl-like formatted status of release objects
func (m *LocalReleaseModule) Status(r *release.Release, req *services.GetReleaseStatusRequest, env *environment.Environment) (string, error) {
	return env.KubeClient.Get(r.Namespace, bytes.NewBufferString(r.Manifest))
}

// Delete deletes the release and returns manifests that were kept in the deletion process
func (m *LocalReleaseModule) Delete(rel *release.Release, req *services.UninstallReleaseRequest, env *environment.Environment) (kept string, errs []error) {
	vs, err := GetVersionSet(m.clientset.Discovery())
	if err != nil {
		return rel.Manifest, []error{fmt.Errorf("Could not get apiVersions from Kubernetes: %v", err)}
	}
	return DeleteRelease(rel, vs, env.KubeClient)
}

// RemoteReleaseModule is a ReleaseModule which calls Rudder service to operate on a release
type RemoteReleaseModule struct{}

// Create calls rudder.InstallRelease
func (m *RemoteReleaseModule) Create(r *release.Release, req *services.InstallReleaseRequest, env *environment.Environment) error {
	request := &rudderAPI.InstallReleaseRequest{Release: r}
	_, err := rudder.InstallRelease(request)
	return err
}

// Update calls rudder.UpgradeRelease
func (m *RemoteReleaseModule) Update(current, target *release.Release, req *services.UpdateReleaseRequest, env *environment.Environment) error {
	upgrade := &rudderAPI.UpgradeReleaseRequest{
		Current:  current,
		Target:   target,
		Recreate: req.Recreate,
		Timeout:  req.Timeout,
		Wait:     req.Wait,
		Force:    req.Force,
	}
	_, err := rudder.UpgradeRelease(upgrade)
	return err
}

// Rollback calls rudder.Rollback
func (m *RemoteReleaseModule) Rollback(current, target *release.Release, req *services.RollbackReleaseRequest, env *environment.Environment) error {
	rollback := &rudderAPI.RollbackReleaseRequest{
		Current:  current,
		Target:   target,
		Recreate: req.Recreate,
		Timeout:  req.Timeout,
		Wait:     req.Wait,
	}
	_, err := rudder.RollbackRelease(rollback)
	return err
}

// Status returns status retrieved from rudder.ReleaseStatus
func (m *RemoteReleaseModule) Status(r *release.Release, req *services.GetReleaseStatusRequest, env *environment.Environment) (string, error) {
	statusRequest := &rudderAPI.ReleaseStatusRequest{Release: r}
	resp, err := rudder.ReleaseStatus(statusRequest)
	if resp == nil {
		return "", err
	}
	return resp.Info.Status.Resources, err
}

// Delete calls rudder.DeleteRelease
func (m *RemoteReleaseModule) Delete(r *release.Release, req *services.UninstallReleaseRequest, env *environment.Environment) (string, []error) {
	deleteRequest := &rudderAPI.DeleteReleaseRequest{Release: r}
	resp, err := rudder.DeleteRelease(deleteRequest)

	errs := make([]error, 0)
	result := ""

	if err != nil {
		errs = append(errs, err)
	}
	if resp != nil {
		result = resp.Release.Manifest
	}
	return result, errs
}

// DeleteRelease is a helper that allows Rudder to delete a release without exposing most of Tiller inner functions
func DeleteRelease(rel *release.Release, vs chartutil.VersionSet, kubeClient environment.KubeClient) (kept string, errs []error) {
	manifests := relutil.SplitManifests(rel.Manifest)
	_, files, err := sortManifests(manifests, vs, UninstallOrder)
	if err != nil {
		// We could instead just delete everything in no particular order.
		// FIXME: One way to delete at this point would be to try a label-based
		// deletion. The problem with this is that we could get a false positive
		// and delete something that was not legitimately part of this release.
		return rel.Manifest, []error{fmt.Errorf("corrupted release record. You must manually delete the resources: %s", err)}
	}

	filesToKeep, filesToDelete := filterManifestsToKeep(files)
	if len(filesToKeep) > 0 {
		kept = summarizeKeptManifests(filesToKeep, kubeClient, rel.Namespace)
	}

	errs = []error{}
	for _, file := range filesToDelete {
		b := bytes.NewBufferString(strings.TrimSpace(file.Content))
		if b.Len() == 0 {
			continue
		}
		if err := kubeClient.Delete(rel.Namespace, b); err != nil {
			log.Printf("uninstall: Failed deletion of %q: %s", rel.Name, err)
			if err == kube.ErrNoObjectsVisited {
				// Rewrite the message from "no objects visited"
				obj := ""
				if file.Head != nil && file.Head.Metadata != nil {
					obj = "[" + file.Head.Kind + "] " + file.Head.Metadata.Name
				}
				err = fmt.Errorf("release %q: object %q not found, skipping delete", rel.Name, obj)
			}
			errs = append(errs, err)
		}
	}
	return kept, errs
}
