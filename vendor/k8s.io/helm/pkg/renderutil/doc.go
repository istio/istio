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

/*Package renderutil contains tools related to the local rendering of charts.

Local rendering means rendering without the tiller; this is generally used for
local debugging and testing (see the `helm template` command for examples of
use).  This package will not render charts exactly the same way as the tiller
will, but will be generally close enough for local debug purposes.
*/
package renderutil // import "k8s.io/helm/pkg/renderutil"
