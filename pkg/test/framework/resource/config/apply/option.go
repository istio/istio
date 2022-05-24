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

package apply

import "istio.io/istio/pkg/test/framework/resource/config/cleanup"

// Option is a strategy for updating Options.
type Option interface {
	// Set this option on the provided Options
	Set(*Options)
}

// OptionFunc is a function-based Option.
type OptionFunc func(*Options)

// Set just invokes this function to update the Options.
func (f OptionFunc) Set(opts *Options) {
	f(opts)
}

// NoCleanup is an Option that disables config cleanup.
var NoCleanup Option = OptionFunc(func(opts *Options) {
	opts.Cleanup = cleanup.None
})

// CleanupConditionally is an Option that sets Cleanup = cleanup.Conditionally.
var CleanupConditionally Option = OptionFunc(func(opts *Options) {
	opts.Cleanup = cleanup.Conditionally
})

// Wait configures the Options to wait for configuration to propagate.
// This method currently does nothing, due to https://github.com/istio/istio/issues/37148
var Wait Option = OptionFunc(func(opts *Options) {
	// opts.Wait = true
})
