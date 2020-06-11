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

package kube

import (
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

// Deployment is a utility that absracts the operation of deploying and deleting
// Kubernetes deployments.
type Deployment struct {
	accessor Accessor

	// The deployment namespace.
	namespace string

	// Path to the yaml file that is generated from the template.
	yamlFilePath string
	yamlContents string

	appliedFiles []string
}

// Deploy this deployment instance.
func (d *Deployment) Deploy(wait bool, opts ...retry.Option) (err error) {
	if d.yamlFilePath != "" {
		if err = d.accessor.Apply(d.namespace, d.yamlFilePath); err != nil {
			return multierror.Prefix(err, "kube apply of generated yaml file:")
		}
	} else {
		if d.appliedFiles, err = d.accessor.ApplyContents(d.namespace, d.yamlContents); err != nil {
			return multierror.Prefix(err, "kube apply of generated yaml file:")
		}
	}

	if wait {
		if _, err := d.accessor.WaitUntilPodsAreReady(NewPodFetch(d.accessor, d.namespace), opts...); err != nil {
			scopes.Framework.Errorf("Wait for Istio pods failed: %v", err)
			return err
		}
	}

	return nil
}

// Delete this deployment instance.
func (d *Deployment) Delete(wait bool, opts ...retry.Option) (err error) {
	if len(d.appliedFiles) > 0 {
		// Delete in the opposite order that they were applied.
		for ix := len(d.appliedFiles) - 1; ix >= 0; ix-- {
			err = multierror.Append(err, d.accessor.Delete(d.namespace, d.appliedFiles[ix])).ErrorOrNil()
		}
	} else if d.yamlFilePath != "" {
		if err = d.accessor.Delete(d.namespace, d.yamlFilePath); err != nil {
			scopes.Framework.Warnf("Error deleting deployment: %v", err)
		}
	} else {
		if err = d.accessor.DeleteContents(d.namespace, d.yamlContents); err != nil {
			scopes.Framework.Warnf("Error deleting deployment: %v", err)
		}
	}

	if wait && err != nil {
		// TODO: Just for waiting for deployment namespace deletion may not be enough. There are CRDs
		// and roles/rolebindings in other parts of the system as well. We should also wait for deletion of them.
		if e := d.accessor.WaitForNamespaceDeletion(d.namespace, opts...); e != nil {
			scopes.Framework.Warnf("Error waiting for environment deletion: %v", e)
			err = multierror.Append(err, e)
		}
	}

	return
}

// NewYamlContentDeployment creates a new deployment from the contents of a yaml document.
func NewYamlContentDeployment(namespace, yamlContents string, a Accessor) *Deployment {
	return &Deployment{
		accessor:     a,
		namespace:    namespace,
		yamlContents: yamlContents,
	}
}
