// Copyright Istio Authors.
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

package verifier

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	v1batch "k8s.io/api/batch/v1"
	apimachinery_schema "k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
)

func verifyDeploymentStatus(deployment *appsv1.Deployment) error {
	cond := getDeploymentCondition(deployment.Status, appsv1.DeploymentProgressing)
	if cond != nil && cond.Reason == "ProgressDeadlineExceeded" {
		return fmt.Errorf("deployment %q exceeded its progress deadline", deployment.Name)
	}
	if deployment.Spec.Replicas != nil && deployment.Status.UpdatedReplicas < *deployment.Spec.Replicas {
		return fmt.Errorf("waiting for deployment %q rollout to finish: %d out of %d new replicas have been updated",
			deployment.Name, deployment.Status.UpdatedReplicas, *deployment.Spec.Replicas)
	}
	if deployment.Status.Replicas > deployment.Status.UpdatedReplicas {
		return fmt.Errorf("waiting for deployment %q rollout to finish: %d old replicas are pending termination",
			deployment.Name, deployment.Status.Replicas-deployment.Status.UpdatedReplicas)
	}
	if deployment.Status.AvailableReplicas < deployment.Status.UpdatedReplicas {
		return fmt.Errorf("waiting for deployment %q rollout to finish: %d of %d updated replicas are available",
			deployment.Name, deployment.Status.AvailableReplicas, deployment.Status.UpdatedReplicas)
	}
	return nil
}

func getDeploymentCondition(status appsv1.DeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

func verifyJobPostInstall(job *v1batch.Job) error {
	for _, c := range job.Status.Conditions {
		if c.Type == v1batch.JobFailed {
			return fmt.Errorf("the required Job %s/%s failed", job.Namespace, job.Name)
		}
	}
	return nil
}

func findResourceInSpec(gvk apimachinery_schema.GroupVersionKind) string {
	s, f := collections.All.FindByGroupVersionKind(config.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	})
	if !f {
		return ""
	}
	return s.Resource().Plural()
}
