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

#ifndef ISTIO_MIXERCLIENT_CHECK_CACHE_H
#define ISTIO_MIXERCLIENT_CHECK_CACHE_H

#include <chrono>
#include <mutex>
#include <string>
#include <unordered_map>
#include <utility>

#include "google/protobuf/stubs/status.h"
#include "proxy/include/istio/mixerclient/client.h"
#include "proxy/include/istio/mixerclient/options.h"
#include "proxy/include/istio/utils/simple_lru_cache.h"
#include "proxy/include/istio/utils/simple_lru_cache_inl.h"
#include "proxy/src/istio/mixerclient/referenced.h"

namespace istio {
namespace mixerclient {

// Cache Mixer Check call result.
// This interface is thread safe.
class CheckCache {
 public:
  CheckCache(const CheckOptions& options);

  virtual ~CheckCache();

  // A check cache result for a request. Its usage
  //   cache->Check(attributes, result);
  //   if (result->IsCacheHit()) return result->Status();
  // Make remote call and on receiving response.
  //   result->SetReponse(status, response);
  //   return result->Status();
  class CheckResult {
   public:
    CheckResult();

    bool IsCacheHit() const;

    ::google::protobuf::util::Status status() const { return status_; }

    ::istio::mixer::v1::RouteDirective route_directive() const {
      return route_directive_;
    }

    void SetResponse(const ::google::protobuf::util::Status& status,
                     const ::istio::mixer::v1::Attributes& attributes,
                     const ::istio::mixer::v1::CheckResponse& response) {
      if (on_response_) {
        status_ = on_response_(status, attributes, response);
      }
      if (response.has_precondition()) {
        route_directive_ = response.precondition().route_directive();
      }
    }

   private:
    friend class CheckCache;
    // Check status.
    ::google::protobuf::util::Status status_;

    // Route directive (if status is OK).
    ::istio::mixer::v1::RouteDirective route_directive_;

    // The function to set check response.
    using OnResponseFunc = std::function<::google::protobuf::util::Status(
        const ::google::protobuf::util::Status&,
        const ::istio::mixer::v1::Attributes& attributes,
        const ::istio::mixer::v1::CheckResponse&)>;
    OnResponseFunc on_response_;
  };

  void Check(const ::istio::mixer::v1::Attributes& attributes,
             CheckResult* result);

 private:
  friend class CheckCacheTest;
  using Tick = std::chrono::time_point<std::chrono::system_clock>;

  // If the check could not be handled by the cache, returns NOT_FOUND,
  // caller has to send the request to mixer.
  ::google::protobuf::util::Status Check(
      const ::istio::mixer::v1::Attributes& request, Tick time_now,
      CheckResult* result);

  // Caches a response from a remote mixer call.
  // Return the converted status from response.
  ::google::protobuf::util::Status CacheResponse(
      const ::istio::mixer::v1::Attributes& attributes,
      const ::istio::mixer::v1::CheckResponse& response, Tick time_now);

  // Flushes out all cached check responses; clears all cache items.
  // Usually called at destructor.
  ::google::protobuf::util::Status FlushAll();

  // Convert from grpc status to protobuf status.
  ::google::protobuf::util::Status ConvertRpcStatus(
      const ::google::rpc::Status& status) const;

  class CacheElem {
   public:
    CacheElem(const CheckCache& parent,
              const ::istio::mixer::v1::CheckResponse& response, Tick time)
        : parent_(parent) {
      SetResponse(response, time);
    }

    // Set the response
    void SetResponse(const ::istio::mixer::v1::CheckResponse& response,
                     Tick time_now);

    // Check if the item is expired.
    bool IsExpired(Tick time_now);

    // getter for converted status from response.
    ::google::protobuf::util::Status status() const { return status_; }

    // getter for the route directive
    ::istio::mixer::v1::RouteDirective route_directive() const {
      return route_directive_;
    }

   private:
    // To the parent cache object.
    const CheckCache& parent_;
    // The check status for the last check request.
    ::google::protobuf::util::Status status_;
    // Route directive
    ::istio::mixer::v1::RouteDirective route_directive_;
    // Cache item should not be used after it is expired.
    std::chrono::time_point<std::chrono::system_clock> expire_time_;
    // if -1, not to check use_count.
    // if 0, cache item should not be used.
    // use_cound is decreased by 1 for each request,
    int use_count_;
  };

  // Key is the signature of the Attributes. Value is the CacheElem.
  // It is a LRU cache with maximum size.
  // When the maximum size is reached, oldest idle items will be removed.
  using CheckLRUCache = utils::SimpleLRUCache<std::string, CacheElem>;

  // The check options.
  CheckOptions options_;

  // Referenced map keyed with their hashes
  std::unordered_map<std::string, Referenced> referenced_map_;

  // Mutex guarding the access of cache_;
  std::mutex cache_mutex_;

  // The cache that maps from operation signature to an operation.
  // We don't calculate fine grained cost for cache entries, assign each
  // entry 1 cost unit.
  // Guarded by mutex_, except when compare against NULL.
  std::unique_ptr<CheckLRUCache> cache_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(CheckCache);
};

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_CHECK_CACHE_H
