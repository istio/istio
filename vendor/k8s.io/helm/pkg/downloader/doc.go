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

/*Package downloader provides a library for downloading charts.

This package contains various tools for downloading charts from repository
servers, and then storing them in Helm-specific directory structures (like
HELM_HOME). This library contains many functions that depend on a specific
filesystem layout.
*/
package downloader
