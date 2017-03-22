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

// Caches check attributes.

#ifndef MIXERCLIENT_CHECK_CACHE_H
#define MIXERCLIENT_CHECK_CACHE_H

#include <mutex>
#include <string>
#include <unordered_map>
#include <utility>

#include "google/protobuf/stubs/status.h"
#include "include/client.h"
#include "include/options.h"
#include "mixer/v1/service.pb.h"
#include "src/signature.h"
#include "utils/simple_lru_cache.h"
#include "utils/simple_lru_cache_inl.h"

namespace istio {
namespace mixer_client {

// Cache Mixer Check Attributes.
// This interface is thread safe.
class CheckCache {
 public:
  CheckCache(const CheckOptions& options);

  virtual ~CheckCache();

  // If the check could not be handled by the cache, returns NOT_FOUND,
  // caller has to send the request to mixer.
  // Otherwise, returns OK and cached response.
  virtual ::google::protobuf::util::Status Check(
      const Attributes& request, ::istio::mixer::v1::CheckResponse* response,
      std::string* signature);

  // Caches a response from a remote mixer call.
  virtual ::google::protobuf::util::Status CacheResponse(
      const std::string& signature,
      const ::istio::mixer::v1::CheckResponse& response);

  // Invalidates expired check responses.
  // Called at time specified by GetNextFlushInterval().
  virtual ::google::protobuf::util::Status Flush();

  // Flushes out all cached check responses; clears all cache items.
  // Usually called at destructor.
  virtual ::google::protobuf::util::Status FlushAll();

 private:
  class CacheElem {
   public:
    CacheElem(const ::istio::mixer::v1::CheckResponse& response,
              const int64_t time)
        : check_response_(response), last_check_time_(time) {}

    // Setter for check response.
    inline void set_check_response(
        const ::istio::mixer::v1::CheckResponse& check_response) {
      check_response_ = check_response;
    }
    // Getter for check response.
    inline const ::istio::mixer::v1::CheckResponse& check_response() const {
      return check_response_;
    }

    // Setter for last check time.
    inline void set_last_check_time(const int64_t last_check_time) {
      last_check_time_ = last_check_time;
    }
    // Getter for last check time.
    inline const int64_t last_check_time() const { return last_check_time_; }

   private:
    // The check response for the last check request.
    ::istio::mixer::v1::CheckResponse check_response_;
    // In general, this is the last time a check response is updated.
    //
    // During flush, we set it to be the request start time to prevent a next
    // check request from triggering another flush. Note that this prevention
    // works only during the flush interval, which means for long RPC, there
    // could be up to RPC_time/flush_interval ongoing check requests.
    int64_t last_check_time_;
  };

  using CacheDeleter = std::function<void(CacheElem*)>;
  // Key is the signature of the Attributes. Value is the CacheElem.
  // It is a LRU cache with MaxIdelTime as response_expiration_time.
  using CheckLRUCache =
      SimpleLRUCacheWithDeleter<std::string, CacheElem, CacheDeleter>;

  // Returns whether we should flush a cache entry.
  // If the aggregated check request is less than flush interval, no need to
  // flush.
  bool ShouldFlush(const CacheElem& elem);

  // Flushes the internal operation in the elem and delete the elem. The
  // response from the server is NOT cached.
  // Takes ownership of the elem.
  void OnCacheEntryDelete(CacheElem* elem);

  // The check options.
  CheckOptions options_;

  // Mutex guarding the access of cache_;
  std::mutex cache_mutex_;

  // The cache that maps from operation signature to an operation.
  // We don't calculate fine grained cost for cache entries, assign each
  // entry 1 cost unit.
  // Guarded by mutex_, except when compare against NULL.
  std::unique_ptr<CheckLRUCache> cache_;

  // flush interval in cycles.
  int64_t flush_interval_in_cycle_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(CheckCache);
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_CHECK_CACHE_H
