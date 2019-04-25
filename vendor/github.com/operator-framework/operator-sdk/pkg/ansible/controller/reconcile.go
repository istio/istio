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

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	ansiblestatus "github.com/operator-framework/operator-sdk/pkg/ansible/controller/status"
	"github.com/operator-framework/operator-sdk/pkg/ansible/events"
	"github.com/operator-framework/operator-sdk/pkg/ansible/proxy/kubeconfig"
	"github.com/operator-framework/operator-sdk/pkg/ansible/runner"
	"github.com/operator-framework/operator-sdk/pkg/ansible/runner/eventapi"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

const (
	// ReconcilePeriodAnnotation - annotation used by a user to specify the reconciliation interval for the CR.
	// To use create a CR with an annotation "ansible.operator-sdk/reconcile-period: 30s" or some other valid
	// Duration. This will override the operators/or controllers reconcile period for that particular CR.
	ReconcilePeriodAnnotation = "ansible.operator-sdk/reconcile-period"
)

// AnsibleOperatorReconciler - object to reconcile runner requests
type AnsibleOperatorReconciler struct {
	GVK             schema.GroupVersionKind
	Runner          runner.Runner
	Client          client.Client
	EventHandlers   []events.EventHandler
	ReconcilePeriod time.Duration
	ManageStatus    bool
}

// Reconcile - handle the event.
func (r *AnsibleOperatorReconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(r.GVK)
	err := r.Client.Get(context.TODO(), request.NamespacedName, u)
	if apierrors.IsNotFound(err) {
		return reconcile.Result{}, nil
	}
	if err != nil {
		return reconcile.Result{}, err
	}

	ident := strconv.Itoa(rand.Int())
	logger := logf.Log.WithName("reconciler").WithValues(
		"job", ident,
		"name", u.GetName(),
		"namespace", u.GetNamespace(),
	)

	reconcileResult := reconcile.Result{RequeueAfter: r.ReconcilePeriod}
	if ds, ok := u.GetAnnotations()[ReconcilePeriodAnnotation]; ok {
		duration, err := time.ParseDuration(ds)
		if err != nil {
			// Should attempt to update to a failed condition
			r.markError(u, request.NamespacedName, fmt.Sprintf("Unable to parse reconcile period annotation: %v", err))
			logger.Error(err, "Unable to parse reconcile period annotation")
			return reconcileResult, err
		}
		reconcileResult.RequeueAfter = duration
	}

	deleted := u.GetDeletionTimestamp() != nil
	finalizer, finalizerExists := r.Runner.GetFinalizer()
	pendingFinalizers := u.GetFinalizers()
	// If the resource is being deleted we don't want to add the finalizer again
	if finalizerExists && !deleted && !contains(pendingFinalizers, finalizer) {
		logger.V(1).Info("Adding finalizer to resource", "Finalizer", finalizer)
		finalizers := append(pendingFinalizers, finalizer)
		u.SetFinalizers(finalizers)
		err := r.Client.Update(context.TODO(), u)
		if err != nil {
			logger.Error(err, "Unable to update cr with finalizer")
			return reconcileResult, err
		}
	}
	if !contains(pendingFinalizers, finalizer) && deleted {
		logger.Info("Resource is terminated, skipping reconciliation")
		return reconcile.Result{}, nil
	}

	spec := u.Object["spec"]
	_, ok := spec.(map[string]interface{})
	// Need to handle cases where there is no spec.
	// We can add the spec to the object, which will allow
	// everything to work, and will not get updated.
	// Therefore we can now deal with the case of secrets and configmaps.
	if !ok {
		logger.V(1).Info("Spec was not found")
		u.Object["spec"] = map[string]interface{}{}
	}

	if r.ManageStatus {
		err = r.markRunning(u, request.NamespacedName)
		if err != nil {
			logger.Error(err, "Unable to update the status to mark cr as running")
			return reconcileResult, err
		}
	}

	ownerRef := metav1.OwnerReference{
		APIVersion: u.GetAPIVersion(),
		Kind:       u.GetKind(),
		Name:       u.GetName(),
		UID:        u.GetUID(),
	}

	kc, err := kubeconfig.Create(ownerRef, "http://localhost:8888", u.GetNamespace())
	if err != nil {
		r.markError(u, request.NamespacedName, "Unable to run reconciliation")
		logger.Error(err, "Unable to generate kubeconfig")
		return reconcileResult, err
	}
	defer func() {
		if err := os.Remove(kc.Name()); err != nil {
			logger.Error(err, "Failed to remove generated kubeconfig file")
		}
	}()
	result, err := r.Runner.Run(ident, u, kc.Name())
	if err != nil {
		r.markError(u, request.NamespacedName, "Unable to run reconciliation")
		logger.Error(err, "Unable to run ansible runner")
		return reconcileResult, err
	}

	// iterate events from ansible, looking for the final one
	statusEvent := eventapi.StatusJobEvent{}
	failureMessages := eventapi.FailureMessages{}
	for event := range result.Events() {
		for _, eHandler := range r.EventHandlers {
			go eHandler.Handle(ident, u, event)
		}
		if event.Event == eventapi.EventPlaybookOnStats {
			// convert to StatusJobEvent; would love a better way to do this
			data, err := json.Marshal(event)
			if err != nil {
				return reconcile.Result{}, err
			}
			err = json.Unmarshal(data, &statusEvent)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
		if event.Event == eventapi.EventRunnerOnFailed && !event.IgnoreError() {
			failureMessages = append(failureMessages, event.GetFailedPlaybookMessage())
		}
	}
	if statusEvent.Event == "" {
		eventErr := errors.New("did not receive playbook_on_stats event")
		stdout, err := result.Stdout()
		if err != nil {
			logger.Error(err, "Failed to get ansible-runner stdout")
			return reconcileResult, err
		}
		logger.Error(eventErr, stdout)
		return reconcileResult, eventErr
	}

	// We only want to update the CustomResource once, so we'll track changes and do it at the end
	runSuccessful := len(failureMessages) == 0
	// The finalizer has run successfully, time to remove it
	if deleted && finalizerExists && runSuccessful {
		finalizers := []string{}
		for _, pendingFinalizer := range pendingFinalizers {
			if pendingFinalizer != finalizer {
				finalizers = append(finalizers, pendingFinalizer)
			}
		}
		u.SetFinalizers(finalizers)
		err := r.Client.Update(context.TODO(), u)
		if err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return reconcileResult, err
		}
	}
	if r.ManageStatus {
		err = r.markDone(u, request.NamespacedName, statusEvent, failureMessages)
		if err != nil {
			logger.Error(err, "Failed to mark status done")
		}
	}
	return reconcileResult, err
}

func (r *AnsibleOperatorReconciler) markRunning(u *unstructured.Unstructured, namespacedName types.NamespacedName) error {
	// Get the latest resource to prevent updating a stale status
	err := r.Client.Get(context.TODO(), namespacedName, u)
	if err != nil {
		return err
	}
	statusInterface := u.Object["status"]
	statusMap, _ := statusInterface.(map[string]interface{})
	crStatus := ansiblestatus.CreateFromMap(statusMap)

	// If there is no current status add that we are working on this resource.
	errCond := ansiblestatus.GetCondition(crStatus, ansiblestatus.FailureConditionType)

	if errCond != nil {
		errCond.Status = v1.ConditionFalse
		ansiblestatus.SetCondition(&crStatus, *errCond)
	}
	// If the condition is currently running, making sure that the values are correct.
	// If they are the same a no-op, if they are different then it is a good thing we
	// are updating it.
	c := ansiblestatus.NewCondition(
		ansiblestatus.RunningConditionType,
		v1.ConditionTrue,
		nil,
		ansiblestatus.RunningReason,
		ansiblestatus.RunningMessage,
	)
	ansiblestatus.SetCondition(&crStatus, *c)
	u.Object["status"] = crStatus.GetJSONMap()
	err = r.Client.Status().Update(context.TODO(), u)
	if err != nil {
		return err
	}
	return nil
}

// markError - used to alert the user to the issues during the validation of a reconcile run.
// i.e Annotations that could be incorrect
func (r *AnsibleOperatorReconciler) markError(u *unstructured.Unstructured, namespacedName types.NamespacedName, failureMessage string) error {
	logger := logf.Log.WithName("markError")
	// Get the latest resource to prevent updating a stale status
	err := r.Client.Get(context.TODO(), namespacedName, u)
	if apierrors.IsNotFound(err) {
		logger.Info("Resource not found, assuming it was deleted", err)
		return nil
	}
	if err != nil {
		return err
	}
	statusInterface := u.Object["status"]
	statusMap, ok := statusInterface.(map[string]interface{})
	// If the map is not available create one.
	if !ok {
		statusMap = map[string]interface{}{}
	}
	crStatus := ansiblestatus.CreateFromMap(statusMap)

	sc := ansiblestatus.GetCondition(crStatus, ansiblestatus.RunningConditionType)
	if sc != nil {
		sc.Status = v1.ConditionFalse
		ansiblestatus.SetCondition(&crStatus, *sc)
	}

	c := ansiblestatus.NewCondition(
		ansiblestatus.FailureConditionType,
		v1.ConditionTrue,
		nil,
		ansiblestatus.FailedReason,
		failureMessage,
	)
	ansiblestatus.SetCondition(&crStatus, *c)
	// This needs the status subresource to be enabled by default.
	u.Object["status"] = crStatus.GetJSONMap()

	return r.Client.Status().Update(context.TODO(), u)
}

func (r *AnsibleOperatorReconciler) markDone(u *unstructured.Unstructured, namespacedName types.NamespacedName, statusEvent eventapi.StatusJobEvent, failureMessages eventapi.FailureMessages) error {
	logger := logf.Log.WithName("markDone")
	// Get the latest resource to prevent updating a stale status
	err := r.Client.Get(context.TODO(), namespacedName, u)
	if apierrors.IsNotFound(err) {
		logger.Info("Resource not found, assuming it was deleted", err)
		return nil
	}
	if err != nil {
		return err
	}
	statusInterface := u.Object["status"]
	statusMap, _ := statusInterface.(map[string]interface{})
	crStatus := ansiblestatus.CreateFromMap(statusMap)

	runSuccessful := len(failureMessages) == 0
	ansibleStatus := ansiblestatus.NewAnsibleResultFromStatusJobEvent(statusEvent)

	if !runSuccessful {
		sc := ansiblestatus.GetCondition(crStatus, ansiblestatus.RunningConditionType)
		sc.Status = v1.ConditionFalse
		ansiblestatus.SetCondition(&crStatus, *sc)
		c := ansiblestatus.NewCondition(
			ansiblestatus.FailureConditionType,
			v1.ConditionTrue,
			ansibleStatus,
			ansiblestatus.FailedReason,
			strings.Join(failureMessages, "\n"),
		)
		ansiblestatus.SetCondition(&crStatus, *c)
	} else {
		c := ansiblestatus.NewCondition(
			ansiblestatus.RunningConditionType,
			v1.ConditionTrue,
			ansibleStatus,
			ansiblestatus.SuccessfulReason,
			ansiblestatus.SuccessfulMessage,
		)
		// Remove the failure condition if set, because this completed successfully.
		ansiblestatus.RemoveCondition(&crStatus, ansiblestatus.FailureConditionType)
		ansiblestatus.SetCondition(&crStatus, *c)
	}
	// This needs the status subresource to be enabled by default.
	u.Object["status"] = crStatus.GetJSONMap()

	return r.Client.Status().Update(context.TODO(), u)
}

func contains(l []string, s string) bool {
	for _, elem := range l {
		if elem == s {
			return true
		}
	}
	return false
}
