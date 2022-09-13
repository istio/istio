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

#include "proxy_wasm_intrinsics.h"

class HeaderInjectorRootContext : public RootContext {
 public:
  explicit HeaderInjectorRootContext(uint32_t id, std::string_view root_id)
      : RootContext(id, root_id) {}

  bool onConfigure(size_t) override;
};

class HeaderInjectorContext : public Context {
 public:
  explicit HeaderInjectorContext(uint32_t id, RootContext* root) : Context(id, root) {}

  FilterHeadersStatus onRequestHeaders(uint32_t, bool) override;
  FilterHeadersStatus onResponseHeaders(uint32_t, bool) override;

 private:
  inline HeaderInjectorRootContext* rootContext() {
    return dynamic_cast<HeaderInjectorRootContext*>(this->root());
  }
};