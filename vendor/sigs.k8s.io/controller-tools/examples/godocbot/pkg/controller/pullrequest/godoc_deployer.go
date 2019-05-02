/*
Copyright 2018 The Kubernetes authors.

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

package pullrequest

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"sigs.k8s.io/controller-tools/examples/godocbot/pkg/apis/code/v1alpha1"
)

// GodocDeployer watches PullRequest object which have a commitID specified in
// their Spec and deploys a Godoc deployment which runs godoc server for the PR.
// It watches the PullRequest object for changes in commitID and reconciles the
// generated godoc deployment. CommitID for PRs is updated by GithubSyncer
// module.
type GodocDeployer struct {
	controller.Controller
}

func NewGodocDeployer(mgr manager.Manager) (*GodocDeployer, error) {
	prReconciler := &pullRequestReconciler{
		Client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}

	// Setup a new controller to Reconcile PullRequests
	c, err := controller.New("pull-request-controller", mgr, controller.Options{Reconcile: prReconciler})
	if err != nil {
		return nil, err
	}

	// Watch PullRequest objects
	err = c.Watch(
		&source.Kind{Type: &v1alpha1.PullRequest{}},
		&handler.Enqueue{})
	if err != nil {
		return nil, err
	}

	// Watch deployments generated for PullRequests objects
	err = c.Watch(
		&source.Kind{Type: &appsv1.Deployment{}},
		&handler.EnqueueOwner{
			OwnerType:    &v1alpha1.PullRequest{},
			IsController: true,
		},
	)
	if err != nil {
		return nil, err
	}

	return &GodocDeployer{Controller: c}, nil
}

// pullRequestReconciler ensures there is a godoc deployment is running with
// the commitID specified in the PullRequest object.
type pullRequestReconciler struct {
	Client client.Client
	scheme *runtime.Scheme
}

func (r *pullRequestReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := context.Background()

	// Fetch PullRequest object
	pr := &v1alpha1.PullRequest{}
	err := r.Client.Get(ctx, request.NamespacedName, pr)
	if errors.IsNotFound(err) {
		log.Printf("Could not find PullRequest %v.\n", request)
		return reconcile.Result{}, nil
	}

	if err != nil {
		log.Printf("Could not fetch PullRequest %v for %+v\n", err, request)
		return reconcile.Result{}, err
	}

	if pr.Spec.CommitID == "" {
		log.Printf("Waiting for PR %s commitID to be updated", request.NamespacedName)
		return reconcile.Result{}, nil
	}

	dp := &appsv1.Deployment{}
	err = r.Client.Get(ctx, request.NamespacedName, dp)
	if err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}

		log.Printf("Could not find deployment for PullRequest %v. creating deployment", request)
		dp, err = deploymentForPullRequest(pr, r.scheme)
		if err != nil {
			log.Printf("error creating new deployment for PullRequest: %v %v", request, err)
			return reconcile.Result{}, nil
		}
		if err = r.Client.Create(ctx, dp); err != nil {
			log.Printf("error creating new deployment for PullRequest: %v %v", request, err)
			return reconcile.Result{}, err
		}
	}

	prinfo, _ := parsePullRequestURL(pr.Spec.URL)
	prinfo.commitID = pr.Spec.CommitID

	if len(dp.Spec.Template.Spec.Containers[0].Args) > 5 && (pr.Spec.CommitID != dp.Spec.Template.Spec.Containers[0].Args[5]) {
		// TODO(droot): publish an event when commit is updated.
		// deployment is not updated with latest commit-id
		dp.Spec.Template.Spec.Containers[0].Args = prinfo.godocContainerArgs()
		if err = r.Client.Update(ctx, dp); err != nil {
			log.Printf("error updating the deployment for key %s", request.NamespacedName)
			return reconcile.Result{}, err
		}
	}

	if pr.Status.GoDocLink == "" && dp.Status.AvailableReplicas > 0 {
		log.Printf("deployment became available, updating the godoc link")
		// update the status
		pr.Status.GoDocLink = fmt.Sprintf("https://%s.serveo.net/pkg/%s/%s/%s", prinfo.subdomain(), prinfo.host, prinfo.org, prinfo.repo)
		if err = r.Client.Update(ctx, pr); err != nil {
			return reconcile.Result{}, err
		}
		log.Printf("godoc link updated successfully for pr %v", request.NamespacedName)
	}
	return reconcile.Result{}, nil
}

// deploymentForPullRequest creates a deployment object for a given PullRequest.
func deploymentForPullRequest(pr *v1alpha1.PullRequest, scheme *runtime.Scheme) (*appsv1.Deployment, error) {
	// we are good with running with one replica
	var replicas int32 = 1

	prinfo, err := parsePullRequestURL(pr.Spec.URL)
	if err != nil {
		return nil, err
	}
	prinfo.commitID = pr.Spec.CommitID

	labels := map[string]string{
		"org":  prinfo.org,
		"repo": prinfo.repo,
	}

	tunnelArgs := "80:localhost:6060"
	tunnelArgs = fmt.Sprintf("%s:%s", prinfo.subdomain(), tunnelArgs)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pr.Name,
			Namespace: pr.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           "gcr.io/sunilarora-sandbox/godoc:0.0.1",
							Name:            "godoc",
							ImagePullPolicy: "Always",
							Command:         []string{"/bin/bash"},
							Args:            prinfo.godocContainerArgs(),
						},
						{
							Image:           "gcr.io/sunilarora-sandbox/ssh-client:0.0.2",
							Name:            "ssh",
							ImagePullPolicy: "Always",
							Command:         []string{"ssh"},
							Args:            []string{"-tt", "-o", "StrictHostKeyChecking=no", "-R", tunnelArgs, "serveo.net"},
						},
					},
				},
			},
		},
	}

	controllerutil.SetControllerReference(pr, dep, scheme)
	return dep, nil
}

// prInfo is an structure to represent PullRequest info. It will be used
// internally to more as an convenience for passing it around.
type prInfo struct {
	host     string
	org      string
	repo     string
	pr       int64
	commitID string
}

// parsePullRequestURL parses given PullRequest URL into prInfo instance.
// An example pull request URL looks like:
// https://github.com/kubernetes-sigs/controller-runtime/pull/15
func parsePullRequestURL(prURL string) (*prInfo, error) {
	u, err := url.Parse(prURL)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" {
		return nil, fmt.Errorf("pr info missing in the URL")
	}

	prNum, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return nil, err
	}

	return &prInfo{
		host: u.Hostname(),
		org:  parts[0],
		repo: parts[1],
		pr:   prNum,
	}, nil
}

// helper function to generate subdomain for the prinfo.
func (pr *prInfo) subdomain() string {
	return fmt.Sprintf("%s-%s-pr-%d", pr.org, pr.repo, pr.pr)
}

func (pr *prInfo) godocContainerArgs() []string {
	return []string{"fetch_serve.sh", pr.host, pr.org, pr.repo, strconv.FormatInt(pr.pr, 10), pr.commitID}
}
