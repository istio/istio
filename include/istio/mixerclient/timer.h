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

#ifndef ISTIO_MIXERCLIENT_TIMER_H
#define ISTIO_MIXERCLIENT_TIMER_H

#include <functional>
#include <memory>

namespace istio {
namespace mixerclient {

// Represent a timer created by caller's environment.
class Timer {
 public:
  // Delete the timer, stopping it first if needed.
  virtual ~Timer() {}

  // Stop a pending timeout without destroying the underlying timer.
  virtual void Stop() = 0;

  // Start a pending timeout. If a timeout is already pending,
  // it will be reset to the new timeout.
  virtual void Start(int interval_ms) = 0;
};

// Defines a function to create a timer calling the function
// with desired interval. The returned object can be used to stop
// the timer.
using TimerCreateFunc =
    std::function<std::unique_ptr<Timer>(std::function<void()> timer_func)>;

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_TIMER_H
