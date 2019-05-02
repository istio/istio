/*
Copyright 2018 The Kubernetes Authors.

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

/*
Package patch provides method to calculate JSON patch between 2 k8s objects.

Calculate JSON patch

	oldDeployment := appsv1.Deployment{
		// some fields
	}
	newDeployment := appsv1.Deployment{
		// some different fields
	}
	patch, err := NewJSONPatch(oldDeployment, newDeployment)
	if err != nil {
		// handle error
	}
*/
package patch
