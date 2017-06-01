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

#ifndef MIXERCLIENT_QUOTA_CACHE_H
#define MIXERCLIENT_QUOTA_CACHE_H

#include <mutex>
#include <string>

#include "include/client.h"
#include "prefetch/quota_prefetch.h"
#include "src/attribute_converter.h"
#include "src/cache_key_set.h"
#include "utils/simple_lru_cache.h"
#include "utils/simple_lru_cache_inl.h"

namespace istio {
namespace mixer_client {

// Cache Mixer Quota Attributes.
// This interface is thread safe.
class QuotaCache {
 public:
  QuotaCache(const QuotaOptions& options, TransportQuotaFunc transport,
             const AttributeConverter& converter);

  virtual ~QuotaCache();

  // Make cached Quota call.
  void Quota(const Attributes& request, DoneFunc on_done);

  // Invalidates expired check responses.
  // Called at time specified by GetNextFlushInterval().
  ::google::protobuf::util::Status Flush();

  // Flushes out all cached check responses; clears all cache items.
  // Usually called at destructor.
  ::google::protobuf::util::Status FlushAll();

 private:
  // The cache element for each quota metric.
  class CacheElem {
   public:
    CacheElem(const ::istio::mixer::v1::QuotaRequest& request,
              TransportQuotaFunc transport);

    // Use the prefetch object to check the quota.
    bool Quota(const Attributes& request);

    // The quota name.
    const std::string& quota_name() const { return request_.quota(); }

   private:
    // The quota allocation call.
    void Alloc(int amount, QuotaPrefetch::DoneFunc fn);

    // The original quota request.
    ::istio::mixer::v1::QuotaRequest request_;
    // The quota transport.
    TransportQuotaFunc transport_;
    // The prefetch object.
    std::unique_ptr<QuotaPrefetch> prefetch_;
  };

  using CacheDeleter = std::function<void(CacheElem*)>;
  // Key is the signature of the Attributes. Value is the CacheElem.
  // It is a LRU cache with MaxIdelTime as response_expiration_time.
  using QuotaLRUCache =
      SimpleLRUCacheWithDeleter<std::string, CacheElem, CacheDeleter>;

  // Convert attributes to protobuf.
  void Convert(const Attributes& attributes, bool best_effort,
               ::istio::mixer::v1::QuotaRequest* request);

  // Flushes the internal operation in the elem and delete the elem. The
  // response from the server is NOT cached.
  // Takes ownership of the elem.
  void OnCacheEntryDelete(CacheElem* elem);

  // The quota options.
  QuotaOptions options_;

  // The quota transport
  TransportQuotaFunc transport_;

  // Attribute converter.
  const AttributeConverter& converter_;

  // The cache keys.
  std::unique_ptr<CacheKeySet> cache_keys_;

  // Mutex guarding the access of cache_;
  std::mutex cache_mutex_;

  // The cache that maps from key to prefetch object
  std::unique_ptr<QuotaLRUCache> cache_;

  // For quota deduplication
  int64_t deduplication_id_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(QuotaCache);
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_CHECK_CACHE_H
