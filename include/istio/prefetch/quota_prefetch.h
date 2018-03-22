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

#ifndef ISTIO_PREFETCH_QUOTA_PREFETCH_H_
#define ISTIO_PREFETCH_QUOTA_PREFETCH_H_

#include <chrono>
#include <functional>
#include <memory>

namespace istio {
namespace prefetch {

// The class to prefetch rate limiting quota.
class QuotaPrefetch {
 public:
  // Define a time stamp type.
  // Ideally, Now() timestamp should be used inside the functions.
  // But for easy unit_test, pass the time in.
  // The input time should be always increasing.
  typedef std::chrono::time_point<std::chrono::system_clock> Tick;

  // Define the options
  struct Options {
    // The predict window to count number of requests and use it
    // as prefetch amount
    std::chrono::milliseconds predict_window;

    // The minimum prefetch amount.
    int min_prefetch_amount;

    // The wait window for the next prefetch if last prefetch is
    // negative. (Its request amount is not granted).
    std::chrono::milliseconds close_wait_window;

    // Constructor with default values.
    Options();
  };

  // Define the transport.
  // The callback function after quota allocation is done from the server.
  // Set amount = -1 If there are any network failures.
  typedef std::function<void(int amount, std::chrono::milliseconds expiration,
                             Tick t)>
      DoneFunc;
  // The transport function to send quota allocation to server.
  typedef std::function<void(int amount, DoneFunc fn, Tick t)> TransportFunc;

  // virtual destructor.
  virtual ~QuotaPrefetch() {}

  // The creator for the prefetch class
  static std::unique_ptr<QuotaPrefetch> Create(TransportFunc transport,
                                               const Options& options, Tick t);

  // Perform a quota check with the amount. Return true if granted.
  virtual bool Check(int amount, Tick t) = 0;
};

}  // namespace prefetch
}  // namespace istio

#endif  // ISTIO_PREFETCH_QUOTA_PREFETCH_H_
