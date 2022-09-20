// Copyright Istio Authors. All Rights Reserved.
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

#include "plugin.h"

#ifndef INJECTION_VERSION
#error INJECTION_VERSION must be defined
#endif // INJECTION_VERSION

#define xstr(s) str(s)
#define str(s) #s

// Boilderplate code to register the extension implementation.
static RegisterContextFactory register_HeaderInjector(CONTEXT_FACTORY(HeaderInjectorContext),
                                               ROOT_FACTORY(HeaderInjectorRootContext));

bool HeaderInjectorRootContext::onConfigure(size_t) { return true; }

FilterHeadersStatus HeaderInjectorContext::onRequestHeaders(uint32_t, bool) {
  addRequestHeader("X-Req-Injection", xstr(INJECTION_VERSION));
  return FilterHeadersStatus::Continue;
}

FilterHeadersStatus HeaderInjectorContext::onResponseHeaders(uint32_t, bool) {
  addResponseHeader("X-Resp-Injection", xstr(INJECTION_VERSION));
  return FilterHeadersStatus::Continue;
}