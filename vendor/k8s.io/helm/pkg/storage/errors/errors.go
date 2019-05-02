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

package errors // import "k8s.io/helm/pkg/storage/errors"
import "fmt"

var (
	// ErrReleaseNotFound indicates that a release is not found.
	ErrReleaseNotFound = func(release string) error { return fmt.Errorf("release: %q not found", release) }
	// ErrReleaseExists indicates that a release already exists.
	ErrReleaseExists = func(release string) error { return fmt.Errorf("release: %q already exists", release) }
	// ErrInvalidKey indicates that a release key could not be parsed.
	ErrInvalidKey = func(release string) error { return fmt.Errorf("release: %q invalid key", release) }
)
