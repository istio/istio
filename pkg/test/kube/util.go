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

/*
Copyright The Kubernetes Authors.

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

package kube

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

func copyRestConfig(cfg *rest.Config) *rest.Config {
	cpy := *cfg
	return &cpy
}

// addSourceToErr adds handleResourcePrefix and source string to error message.
// verb is the string like "creating", "deleting" etc.
// source is the filename or URL to the template file(*.json or *.yaml), or stdin to use to handle the resource.
func addSourceToErr(verb string, source string, err error) error {
	if source != "" {
		if statusError, ok := err.(apierrors.APIStatus); ok {
			status := statusError.Status()
			status.Message = fmt.Sprintf("error when %s %q: %v", verb, source, status.Message)
			return &apierrors.StatusError{ErrStatus: status}
		}
		return fmt.Errorf("error when %s %q: %v", verb, source, err)
	}
	return err
}
