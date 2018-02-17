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

#ifndef ISTIO_PREFETCH_TIME_BASED_COUNTER_H_
#define ISTIO_PREFETCH_TIME_BASED_COUNTER_H_

#include <chrono>
#include <vector>

namespace istio {
namespace prefetch {

// Define a counter for the count in a time based window.
// Each count is associated with a time stamp. The count outside
// of the window will not be counted.
class TimeBasedCounter {
 public:
  // Define a time stamp type.
  // Ideally, Now() timestamp should be used inside the functions.
  // But for easy unit_test, pass the time in.
  // The input time should be always increasing.
  typedef std::chrono::time_point<std::chrono::system_clock> Tick;

  TimeBasedCounter(int window_size, std::chrono::milliseconds duration, Tick t);

  // Add n count to the counter.
  void Inc(int n, Tick t);

  // Get the count.
  int Count(Tick t);

 private:
  // Clear the whole window
  void Clear(Tick t);
  // Roll the window
  void Roll(Tick t);

  std::vector<int> slots_;
  std::chrono::milliseconds slot_duration_;
  int count_;
  int tail_;
  Tick last_time_;
};

}  // namespace prefetch
}  // namespace istio

#endif  // ISTIO_CLIENT_PREFETCH_TIME_BASED_COUNTER_H_
