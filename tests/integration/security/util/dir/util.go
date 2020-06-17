//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package dir

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/tests/util"
)

// ListDir lists the given directory on a pod and calls the validation function on
// the output.
func ListDir(ns namespace.Instance, t *testing.T, labelSelector, container, directory string, validate func(string) error) {
	retry := util.Retrier{
		BaseDelay: 10 * time.Second,
		Retries:   3,
		MaxDelay:  30 * time.Second,
	}

	podName, err := GetPodName(ns, labelSelector)
	if err != nil {
		t.Errorf("err getting pod name: %v", err)
		return
	}
	retryFn := func(_ context.Context, i int) error {
		execCmd := fmt.Sprintf(
			"kubectl exec -it %s -c %s -n %s -- ls -la %s",
			podName, container, ns.Name(), directory)
		out, err := shell.Execute(false,
			execCmd)
		if err != nil {
			return fmt.Errorf("error executing the cmd (%v): %v", execCmd, err)
		}
		err = validate(out)
		if err != nil {
			return fmt.Errorf("error validating the output (%v): %v", out, err)
		}
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		t.Errorf("ListDir retry failed with err: %v", err)
	}
}

func GetPodName(ns namespace.Instance, labelSelector string) (string, error) {
	retry := util.Retrier{
		BaseDelay: 10 * time.Second,
		Retries:   3,
		MaxDelay:  30 * time.Second,
	}
	var podName string

	retryFn := func(_ context.Context, i int) error {
		podCmd := fmt.Sprintf(
			"kubectl get pod -l %s -n %s -o jsonpath='{.items[0].metadata.name}'",
			labelSelector, ns.Name())
		out, err := shell.Execute(false,
			podCmd)
		if err != nil {
			return fmt.Errorf("error executing the cmd (%v): %v", podCmd, err)
		}
		podName = strings.Trim(out, "'")
		return nil
	}

	if _, err := retry.Retry(context.Background(), retryFn); err != nil {
		return podName, fmt.Errorf("getPodName retry failed with err: %v", err)
	}
	return podName, nil
}
