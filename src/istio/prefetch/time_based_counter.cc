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

#include "src/istio/prefetch/time_based_counter.h"

using namespace std::chrono;

namespace istio {
namespace prefetch {

TimeBasedCounter::TimeBasedCounter(int window_size, milliseconds duration,
                                   Tick t)
    : slots_(window_size),
      slot_duration_(duration / window_size),
      count_(0),
      tail_(0),
      last_time_(t) {}

void TimeBasedCounter::Clear(Tick t) {
  last_time_ = t;
  for (size_t i = 0; i < slots_.size(); i++) {
    slots_[i] = 0;
  }
  tail_ = count_ = 0;
}

void TimeBasedCounter::Roll(Tick t) {
  auto d = duration_cast<milliseconds>(t - last_time_);
  uint32_t n = uint32_t(d.count() / slot_duration_.count());
  if (n >= slots_.size()) {
    Clear(t);
    return;
  }

  for (uint32_t i = 0; i < n; i++) {
    tail_ = (tail_ + 1) % slots_.size();
    count_ -= slots_[tail_];
    slots_[tail_] = 0;
    last_time_ += slot_duration_;
  }
}

void TimeBasedCounter::Inc(int n, Tick t) {
  Roll(t);
  slots_[tail_] += n;
  count_ += n;
}

int TimeBasedCounter::Count(Tick t) {
  Roll(t);
  return count_;
}

}  // namespace prefetch
}  // namespace istio
