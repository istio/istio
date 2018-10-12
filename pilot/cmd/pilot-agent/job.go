// Copyright 2018 Istio Authors
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
	"context"
	"fmt"
	"os"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/log"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"k8s.io/client-go/tools/cache"
)

func monitorJob(ctx context.Context, stopCh chan struct{}) {
	client, err := kube.CreateClientset("", "")
	if err != nil {
		log.Infof("Job monitor: Unable to monitor for a k8s Job; Error getting a k8s client: %v", err)
		return
	}

	pod, err := client.CoreV1().Pods(os.Getenv("POD_NAMESPACE")).Get(os.Getenv("POD_NAME"), metav1.GetOptions{})
	if err != nil {
		log.Infof("Job monitor: Unable to monitor for a k8s Job; Error getting the current pod: %v", err)
		return
	}

	isJob, kind, jobName := getJobDetails(pod)
	if isJob {
		log.Infof("Job monitor: This pod is owned by %s '%s', starting monitoring", kind, jobName)
	} else {
		log.Info("Job monitor: This pod is not owned by a Job, not monitoring it")
		return
	}

	containersToWatch := make(map[string]bool)

	for _, container := range pod.Spec.Containers {
		if container.Name != "istio-proxy" {
			containersToWatch[container.Name] = true
		}
	}
	log.Infof("Job monitor: Watching for %d container(s). Will shutdown when all of them are terminated.", len(containersToWatch))

	stopWatch := make(chan struct{})
	stopInternal := make(chan struct{})
	watchlist := cache.NewListWatchFromClient(client.CoreV1().RESTClient(),
		"pods",
		os.Getenv("POD_NAMESPACE"),
		fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", os.Getenv("POD_NAME"))))
	_, controller := cache.NewInformer(watchlist,
		&v1.Pod{},
		0,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				pod := newObj.(*v1.Pod)
				for _, status := range pod.Status.ContainerStatuses {
					if !containersToWatch[status.Name] {
						continue
					}

					if status.State.Terminated != nil {
						delete(containersToWatch, status.Name)
						log.Infof("Job monitor: Container %s terminated.", status.Name)
					}

					if len(containersToWatch) == 0 {
						log.Infof("Job monitor: All containers in %s '%s' have terminated, exiting.", kind, jobName)
						stopWatch <- struct{}{}
						stopInternal <- struct{}{}
					}
				}
			},
		},
	)

	go controller.Run(stopWatch)
	log.Info("Job started controller, about to select{}")

	select {
	case <-ctx.Done():
		stopWatch <- struct{}{}
		return
	case <-stopInternal:
		stopCh <- struct{}{}
		return
	}
}

func getJobDetails(pod *v1.Pod) (isJob bool, kind string, name string) {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "Job" || ref.Kind == "CronJob" {
			isJob = true
			kind = ref.Kind
			name = ref.Name
			return
		}
	}

	return false, "", ""
}
