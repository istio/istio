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

#ifndef MIXERCLIENT_OPTIONS_H
#define MIXERCLIENT_OPTIONS_H

#include <memory>
#include <set>
#include <vector>

namespace istio {
namespace mixer_client {

// Options controlling check behavior.
struct CheckOptions {
  // Default constructor.
  // Default options are chosen from experience.
  CheckOptions()
      : num_entries(10000), flush_interval_ms(500), expiration_ms(1000) {}

  // Constructor.
  // cache_entries is the maximum number of cache entries that can be kept in
  // the cache. Cache is disabled when cache_entries <= 0.
  // flush_cache_entry_interval_ms is the maximum milliseconds before an
  // check request needs to send to remote server again.
  // response_expiration_ms is the maximum milliseconds before a cached check
  // response is invalidated. We make sure that it is at least
  // flush_cache_entry_interval_ms + 1.
  CheckOptions(int cache_entries, int flush_cache_entry_interval_ms,
               int response_expiration_ms)
      : num_entries(cache_entries),
        flush_interval_ms(flush_cache_entry_interval_ms),
        expiration_ms(std::max(flush_cache_entry_interval_ms + 1,
                               response_expiration_ms)) {}

  // Maximum number of cache entries kept in the cache.
  // Set to 0 will disable caching.
  const int num_entries;

  // Maximum milliseconds before check requests are flushed to the
  // server. The flush is triggered by a check request.
  const int flush_interval_ms;

  // Maximum milliseconds before a cached check response should be deleted. The
  // deletion is triggered by a timer. This value must be larger than
  // flush_interval_ms.
  const int expiration_ms;

  // Only the attributes in this set are used to caclculate cache key.
  // If empty, check cache is disabled.
  std::vector<std::string> cache_keys;
};

// Options controlling quota behavior.
struct QuotaOptions {
  // Default constructor.
  QuotaOptions() : num_entries(10000), expiration_ms(600000) {}

  // Constructor.
  // cache_entries is the maximum number of cache entries that can be kept in
  // the cache. Cache is disabled when cache_entries <= 0.
  // expiration_ms is the maximum milliseconds an idle cached quota is removed.
  QuotaOptions(int cache_entries, int expiration_ms)
      : num_entries(cache_entries), expiration_ms(expiration_ms) {}

  // Maximum number of cache entries kept in the cache.
  // Set to 0 will disable caching.
  const int num_entries;

  // Maximum milliseconds before an idle cached quota should be deleted.
  const int expiration_ms;
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_OPTIONS_H
