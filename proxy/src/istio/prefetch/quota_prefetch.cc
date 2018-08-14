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

#include "include/istio/prefetch/quota_prefetch.h"
#include "src/istio/prefetch/circular_queue.h"
#include "src/istio/prefetch/time_based_counter.h"

#include <mutex>

using namespace std::chrono;

// Turn this on to debug for quota_prefetch_test.cc
// Not for debugging in production.
#if 0
#include <iostream>
#define LOG(t)                                                           \
  std::cerr << "("                                                       \
            << duration_cast<milliseconds>(t.time_since_epoch()).count() \
            << "):"
#else
// Pipe to stringstream to disable logging.
#include <sstream>
std::ostringstream os;
#define LOG(t) \
  os.clear();  \
  os
#endif

namespace istio {
namespace prefetch {
namespace {

// Default predict window size in milliseconds.
const int kPredictWindowInMs = 1000;

// Default min prefetch amount.
const int kMinPrefetchAmount = 10;

// Default close wait window in milliseconds.
const int kCloseWaitWindowInMs = 500;

// Initiail Circular Queue size for prefetch pool.
const int kInitQueueSize = 10;

// TimeBasedCounter window size
const int kTimeBasedWindowSize = 20;

// Maximum expiration for prefetch amount.
// It is only used when a prefetch amount is added to the pool
// before it is granted. Usually is 1 minute.
const int kMaxExpirationInMs = 60000;

// The implementation class to hide internal implementation detail.
class QuotaPrefetchImpl : public QuotaPrefetch {
 public:
  // The slot id type.
  typedef uint64_t SlotId;

  // The struture to store granted amount.
  struct Slot {
    // available amount
    int available;
    // the time the amount will be expired.
    Tick expire_time;
    // the always increment ID to detect if a Slot has been re-cycled.
    SlotId id;
  };

  // The mode.
  enum Mode {
    OPEN = 0,
    CLOSE,
  };

  QuotaPrefetchImpl(TransportFunc transport, const Options& options, Tick t)
      : queue_(kInitQueueSize),
        counter_(kTimeBasedWindowSize, options.predict_window, t),
        mode_(OPEN),
        inflight_count_(0),
        transport_(transport),
        options_(options),
        next_slot_id_(0) {}

  bool Check(int amount, Tick t) override;

 private:
  // Count available token
  int CountAvailable(Tick t);
  // Check available count is bigger than minimum
  int CheckMinAvailable(int min, Tick t);
  // Check to see if need to do a prefetch.
  void AttemptPrefetch(int amount, Tick t);
  // Make a prefetch call.
  void Prefetch(int req_amount, bool use_not_granted, Tick t);
  // Add the amount to the queue, and return slot id.
  SlotId Add(int amount, Tick expiration);
  // Substract the amount from the queue.
  // Return the amount that could not be substracted.
  int Substract(int delta, Tick t);
  // On quota allocation response.
  void OnResponse(SlotId slot_id, int req_amount, int resp_amount,
                  milliseconds expiration, Tick t);
  // Find the slot by id.
  Slot* FindSlotById(SlotId id);

  // The mutex guarding all member variables.
  std::mutex mutex_;
  // The FIFO queue to store prefetched amount.
  CircularQueue<Slot> queue_;
  // The counter to count number of requests in the pass window.
  TimeBasedCounter counter_;
  // The current mode.
  Mode mode_;
  // Last prefetch time.
  Tick last_prefetch_time_;
  // inflight request count;
  int inflight_count_;
  // The transport to allocate quota.
  TransportFunc transport_;
  // Save the options.
  Options options_;
  // next slot id
  SlotId next_slot_id_;
};

int QuotaPrefetchImpl::CountAvailable(Tick t) {
  int avail = 0;
  queue_.Iterate([&](Slot& slot) -> bool {
    if (t < slot.expire_time) {
      avail += slot.available;
    }
    return true;
  });
  return avail;
}

int QuotaPrefetchImpl::CheckMinAvailable(int min, Tick t) {
  int avail = 0;
  queue_.Iterate([&](Slot& slot) -> bool {
    if (t < slot.expire_time) {
      avail += slot.available;
      if (avail >= min) return false;
    }
    return true;
  });
  return avail >= min;
}

void QuotaPrefetchImpl::AttemptPrefetch(int amount, Tick t) {
  if (mode_ == CLOSE && (inflight_count_ > 0 ||
                         (duration_cast<milliseconds>(t - last_prefetch_time_) <
                          options_.close_wait_window))) {
    return;
  }

  int avail = CountAvailable(t);
  int pass_count = counter_.Count(t);
  int desired = std::max(pass_count, options_.min_prefetch_amount);
  if ((avail < desired / 2 && inflight_count_ == 0) || avail < amount) {
    bool use_not_granted = (avail == 0 && mode_ == OPEN);
    Prefetch(std::max(amount, desired), use_not_granted, t);
  }
}

void QuotaPrefetchImpl::Prefetch(int req_amount, bool use_not_granted, Tick t) {
  SlotId slot_id = 0;
  if (use_not_granted) {
    // add the prefetch amount to available queue before it is granted.
    slot_id = Add(req_amount, t + milliseconds(kMaxExpirationInMs));
  }

  LOG(t) << "Prefetch: " << req_amount << ", id: " << slot_id << std::endl;

  last_prefetch_time_ = t;
  ++inflight_count_;
  transport_(req_amount,
             [this, slot_id, req_amount](int resp_amount,
                                         milliseconds expiration, Tick t1) {
               OnResponse(slot_id, req_amount, resp_amount, expiration, t1);
             },
             t);
}

QuotaPrefetchImpl::Slot* QuotaPrefetchImpl::FindSlotById(SlotId id) {
  Slot* found = nullptr;
  queue_.Iterate([&](Slot& slot) -> bool {
    if (slot.id == id) {
      found = &slot;
      return false;
    }
    return true;
  });
  return found;
}

QuotaPrefetchImpl::SlotId QuotaPrefetchImpl::Add(int amount, Tick expire_time) {
  SlotId id = ++next_slot_id_;
  queue_.Push(Slot{amount, expire_time, id});
  return id;
}

int QuotaPrefetchImpl::Substract(int delta, Tick t) {
  Slot* n = queue_.Head();
  while (n != nullptr && delta > 0) {
    if (t < n->expire_time) {
      if (n->available > 0) {
        int d = std::min(n->available, delta);
        n->available -= d;
        delta -= d;
      }
      if (n->available > 0) {
        return 0;
      }
    } else {
      if (n->available > 0) {
        LOG(t) << "Expired:" << n->available << std::endl;
      }
    }
    queue_.Pop();
    n = queue_.Head();
  }
  return delta;
}

void QuotaPrefetchImpl::OnResponse(SlotId slot_id, int req_amount,
                                   int resp_amount, milliseconds expiration,
                                   Tick t) {
  std::lock_guard<std::mutex> lock(mutex_);
  --inflight_count_;

  LOG(t) << "OnResponse: req:" << req_amount << ", resp: " << resp_amount
         << ", expire: " << expiration.count() << ", id: " << slot_id
         << std::endl;

  // resp_amount of -1 indicates any network failures.
  // Use fail open policy to handle any netowrk failures.
  if (resp_amount == -1) {
    resp_amount = req_amount;
    expiration = milliseconds(kMaxExpirationInMs);
  }

  Slot* slot = nullptr;
  if (slot_id != 0) {
    // The prefetched amount was added to the available queue
    slot = FindSlotById(slot_id);
    if (resp_amount < req_amount) {
      int delta = req_amount - resp_amount;
      // Substract it from its own request node.
      if (slot != nullptr) {
        int d = std::min(slot->available, delta);
        slot->available -= d;
        delta -= d;
      }
      if (delta > 0) {
        // Substract it from other prefetched amounts
        Substract(delta, t);
      }
    }
    // Adjust the expiration
    if (slot != nullptr && slot->available > 0) {
      slot->expire_time = t + expiration;
    }
  } else {
    // prefetched amount was NOT added to the pool yet.
    if (resp_amount > 0) {
      Add(resp_amount, t + expiration);
    }
  }

  if (resp_amount == req_amount) {
    mode_ = OPEN;
  } else {
    mode_ = CLOSE;
  }
}

bool QuotaPrefetchImpl::Check(int amount, Tick t) {
  std::lock_guard<std::mutex> lock(mutex_);

  AttemptPrefetch(amount, t);
  counter_.Inc(amount, t);
  bool ret;
  if (amount == 1) {
    ret = Substract(amount, t) == 0;
  } else {
    ret = CheckMinAvailable(amount, t);
    if (ret) {
      Substract(amount, t);
    }
  }
  if (!ret) {
    LOG(t) << "Rejected amount: " << amount << std::endl;
  }
  return ret;
}

}  // namespace

// Constructor with default values.
QuotaPrefetch::Options::Options()
    : predict_window(kPredictWindowInMs),
      min_prefetch_amount(kMinPrefetchAmount),
      close_wait_window(kCloseWaitWindowInMs) {}

std::unique_ptr<QuotaPrefetch> QuotaPrefetch::Create(TransportFunc transport,
                                                     const Options& options,
                                                     Tick t) {
  return std::unique_ptr<QuotaPrefetch>(
      new QuotaPrefetchImpl(transport, options, t));
}

}  // namespace prefetch
}  // namespace istio
