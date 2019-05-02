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

package plugin // import "k8s.io/helm/pkg/plugin"

// Types of hooks
const (
	// Install is executed after the plugin is added.
	Install = "install"
	// Delete is executed after the plugin is removed.
	Delete = "delete"
	// Update is executed after the plugin is updated.
	Update = "update"
)

// Hooks is a map of events to commands.
type Hooks map[string]string

// Get returns a hook for an event.
func (hooks Hooks) Get(event string) string {
	h, _ := hooks[event]
	return h
}
