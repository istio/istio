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

#include "src/check_cache.h"
#include "src/signature.h"
#include "utils/protobuf.h"

using namespace std::chrono;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

void CheckCache::CacheElem::CacheElem::SetResponse(
    const CheckResponse& response, Tick time_now) {
  status_ = ConvertRpcStatus(response.status());
  if (response.has_cachability()) {
    if (response.cachability().has_duration()) {
      expire_time_ =
          time_now + ToMilliseonds(response.cachability().duration());
    } else {
      // never expired.
      expire_time_ = time_point<system_clock>::max();
    }
    use_count_ = response.cachability().use_count();
  } else {
    // if no cachability specified, use it forever.
    use_count_ = -1;  // -1 for not checking use_count
    expire_time_ = time_point<system_clock>::max();
  }
}

// check if the item is expired.
bool CheckCache::CacheElem::CacheElem::IsExpired(Tick time_now) {
  if (time_now > expire_time_ || use_count_ == 0) {
    return true;
  }
  if (use_count_ > 0) {
    --use_count_;
  }
  return false;
}

CheckCache::CheckCache(const CheckOptions& options) : options_(options) {
  if (options.num_entries > 0 && !options_.cache_keys.empty()) {
    cache_.reset(new CheckLRUCache(options.num_entries));
    cache_keys_ = CacheKeySet::CreateInclusive(options_.cache_keys);
  }
}

CheckCache::~CheckCache() {
  // FlushAll() will remove all cache items.
  FlushAll();
}

Status CheckCache::Check(const Attributes& attributes, Tick time_now,
                         std::string* ret_signature) {
  if (!cache_) {
    // By returning NOT_FOUND, caller will send request to server.
    return Status(Code::NOT_FOUND, "");
  }

  std::string signature = GenerateSignature(attributes, *cache_keys_);
  if (ret_signature) {
    *ret_signature = signature;
  }

  std::lock_guard<std::mutex> lock(cache_mutex_);
  CheckLRUCache::ScopedLookup lookup(cache_.get(), signature);

  if (!lookup.Found()) {
    // By returning NO_FOUND, caller will send request to server.
    return Status(Code::NOT_FOUND, "");
  }

  CacheElem* elem = lookup.value();
  if (elem->IsExpired(time_now)) {
    // By returning NO_FOUND, caller will send request to server.
    cache_->Remove(signature);
    return Status(Code::NOT_FOUND, "");
  }
  return elem->status();
}

Status CheckCache::CacheResponse(const std::string& signature,
                                 const CheckResponse& response, Tick time_now) {
  if (!cache_) {
    return ConvertRpcStatus(response.status());
  }

  std::lock_guard<std::mutex> lock(cache_mutex_);
  CheckLRUCache::ScopedLookup lookup(cache_.get(), signature);

  if (lookup.Found()) {
    lookup.value()->SetResponse(response, time_now);
    return lookup.value()->status();
  }

  CacheElem* cache_elem = new CacheElem(response, time_now);
  cache_->Insert(signature, cache_elem, 1);
  return cache_elem->status();
}

// Flush out aggregated check requests, clear all cache items.
// Usually called at destructor.
Status CheckCache::FlushAll() {
  if (cache_) {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    cache_->RemoveAll();
  }

  return Status::OK;
}

}  // namespace mixer_client
}  // namespace istio
