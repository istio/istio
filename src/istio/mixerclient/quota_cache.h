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

// Caches quota calls.

#ifndef ISTIO_MIXERCLIENT_QUOTA_CACHE_H
#define ISTIO_MIXERCLIENT_QUOTA_CACHE_H

#include <mutex>
#include <string>
#include <unordered_map>

#include "include/istio/mixerclient/client.h"
#include "include/istio/prefetch/quota_prefetch.h"
#include "include/istio/utils/simple_lru_cache.h"
#include "include/istio/utils/simple_lru_cache_inl.h"
#include "src/istio/mixerclient/referenced.h"

namespace istio {
namespace mixerclient {

// Cache Mixer Quota Attributes.
// This interface is thread safe.
class QuotaCache {
 public:
  QuotaCache(const QuotaOptions& options);

  virtual ~QuotaCache();

  // A class to batch multiple quota requests.
  // Its usage:
  //     cache->Quota(attributes, &result);
  //     send = result->BuildRequest(&request);
  //     if (cache->IsCacheHit()) return result->Result();
  // If send is true, make a remote call, on response.
  //     result->SetResponse(status, response);
  //     return result->Result();
  class CheckResult {
   public:
    CheckResult();

    // Build CheckRequest::quotas fields, return true if remote quota call
    // is required.
    bool BuildRequest(::istio::mixer::v1::CheckRequest* request);

    bool IsCacheHit() const;

    ::google::protobuf::util::Status status() const { return status_; }

    void SetResponse(const ::google::protobuf::util::Status& status,
                     const ::istio::mixer::v1::Attributes& attributes,
                     const ::istio::mixer::v1::CheckResponse& response);

   private:
    friend class QuotaCache;
    // Hold pending quota data needed to talk to server.
    struct Quota {
      std::string name;
      int64_t amount;
      bool best_effort;

      enum Result {
        Pending = 0,
        Passed,
        Rejected,
      };
      Result result;

      // The function to set the quota response from server.
      using OnResponseFunc = std::function<bool(
          const ::istio::mixer::v1::Attributes& attributes,
          const ::istio::mixer::v1::CheckResponse::QuotaResult* result)>;
      OnResponseFunc response_func;
    };

    ::google::protobuf::util::Status status_;

    // The list of pending quota needed to talk to server.
    std::vector<Quota> quotas_;
  };

  // Check quota cache for a request, result will be stored in CacaheResult.
  void Check(const ::istio::mixer::v1::Attributes& request,
             const std::vector<::istio::quota_config::Requirement>& quotas,
             bool use_cache, CheckResult* result);

 private:
  // Check quota cache.
  void CheckCache(const ::istio::mixer::v1::Attributes& request, bool use_cache,
                  CheckResult::Quota* quota);

  // Invalidates expired check responses.
  // Called at time specified by GetNextFlushInterval().
  ::google::protobuf::util::Status Flush();

  // Flushes out all cached check responses; clears all cache items.
  // Usually called at destructor.
  ::google::protobuf::util::Status FlushAll();

  // The cache element for each quota metric.
  class CacheElem {
   public:
    CacheElem(const std::string& name);

    // Use the prefetch object to check the quota.
    void Quota(int amount, CheckResult::Quota* quota);

    // The quota name.
    const std::string& quota_name() const { return name_; }

   private:
    // The quota allocation call.
    void Alloc(int amount, prefetch::QuotaPrefetch::DoneFunc fn);

    std::string name_;

    // A temporary pending quota result.
    CheckResult::Quota* quota_;

    // The prefetch object.
    std::unique_ptr<prefetch::QuotaPrefetch> prefetch_;
  };

  // Per quota Referenced data.
  struct PerQuotaReferenced {
    // Pending CacheElem for all cache miss requests.
    // This item will be added to the cache after response.
    std::unique_ptr<CacheElem> pending_item;

    // Referenced map keyed with their hashes
    std::unordered_map<std::string, Referenced> referenced_map;
  };

  // Set a quota response.
  void SetResponse(
      const ::istio::mixer::v1::Attributes& attributes,
      const std::string& quota_name,
      const ::istio::mixer::v1::CheckResponse::QuotaResult* result);

  // A map from quota name to PerQuotaReferenced.
  std::unordered_map<std::string, PerQuotaReferenced> quota_referenced_map_;

  // Key is the signature of the Attributes. Value is the CacheElem.
  // It is a LRU cache with MaxIdelTime as response_expiration_time.
  using QuotaLRUCache = utils::SimpleLRUCache<std::string, CacheElem>;

  // The quota options.
  QuotaOptions options_;

  // Mutex guarding the access of cache_ and quota_referenced_map_
  std::mutex cache_mutex_;

  // The cache that maps from key to prefetch object
  std::unique_ptr<QuotaLRUCache> cache_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(QuotaCache);
};

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_QUOTA_CACHE_H
