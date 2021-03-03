// Copyright Istio Authors
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

package server

// Mode indicates how the server is started.
type Mode string

const (
	// Release implies that the server is started in production mode and no unsafe/debug features are enabled.
	Release Mode = "Release"
	// Debug implies that the server is started in debug mode and unsafe features like assertion can be enabled.
	Debug Mode = "Debug"
)
