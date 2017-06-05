/* Copyright 2017 Istio Authors. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

#include "src/envoy/mixer/thread_dispatcher.h"
#include "common/common/assert.h"

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// A thread local dispatcher.
thread_local Event::Dispatcher* thread_dispatcher = nullptr;

}  // namespace

void SetThreadDispatcher(Event::Dispatcher& dispatcher) {
  if (!thread_dispatcher) {
    thread_dispatcher = &dispatcher;
  }
}

Event::Dispatcher& GetThreadDispatcher() {
  // If assert fail, this function is called at the wrong place.
  // It should be called at request processing,
  // after SetThreadDispatcher which is called at DecodeHeader.
  ASSERT(thread_dispatcher);
  return *thread_dispatcher;
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
