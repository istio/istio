/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef API_MANAGER_PERIODIC_TIMER_H_
#define API_MANAGER_PERIODIC_TIMER_H_

#include "contrib/endpoints/include/api_manager/utils/status.h"

namespace google {
namespace api_manager {

// Represents a periodic timer created by API Manager's environment.
class PeriodicTimer {
 public:
  // Deletes the timer, stopping it first if needed.  This method
  // synchronizes with any outstanding invocation of the timer's
  // callback function; this method must not be invoked from within
  // the callback (doing so will cause undefined behavior).
  virtual ~PeriodicTimer() {}

  // Stops the timer, preventing new invocations of the timer's
  // callback function from being started.  This method does not
  // synchronize with outstanding invocations of the timer's callback
  // function; it may be invoked from within the callback.
  virtual void Stop() = 0;
};

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_PERIODIC_TIMER_H_
