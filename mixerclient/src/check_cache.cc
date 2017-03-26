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

#include "google/protobuf/stubs/logging.h"

using std::string;
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

CheckCache::CheckCache(const CheckOptions& options)
    : options_(options), cache_keys_(options.cache_keys) {
  // Converts flush_interval_ms to Cycle used by SimpleCycleTimer.
  flush_interval_in_cycle_ =
      options_.flush_interval_ms * SimpleCycleTimer::Frequency() / 1000;

  if (options.num_entries >= 0 && !cache_keys_.empty()) {
    cache_.reset(new CheckLRUCache(
        options.num_entries, std::bind(&CheckCache::OnCacheEntryDelete, this,
                                       std::placeholders::_1)));
    cache_->SetMaxIdleSeconds(options.expiration_ms / 1000.0);
  }
}

CheckCache::~CheckCache() {
  // FlushAll() will remove all cache items.
  FlushAll();
}

bool CheckCache::ShouldFlush(const CacheElem& elem) {
  int64_t age = SimpleCycleTimer::Now() - elem.last_check_time();
  return age >= flush_interval_in_cycle_;
}

Status CheckCache::Check(const Attributes& attributes, CheckResponse* response,
                         std::string* signature) {
  if (!cache_) {
    // By returning NOT_FOUND, caller will send request to server.
    return Status(Code::NOT_FOUND, "");
  }

  std::string request_signature = GenerateSignature(attributes, cache_keys_);
  if (signature) {
    *signature = request_signature;
  }

  std::lock_guard<std::mutex> lock(cache_mutex_);
  CheckLRUCache::ScopedLookup lookup(cache_.get(), request_signature);

  if (!lookup.Found()) {
    // By returning NO_FOUND, caller will send request to server.
    return Status(Code::NOT_FOUND, "");
  }

  CacheElem* elem = lookup.value();

  if (ShouldFlush(*elem)) {
    // Setting last check to now to block more check requests to Mixer.
    elem->set_last_check_time(SimpleCycleTimer::Now());
    // By returning NO_FOUND, caller will send request to server.
    return Status(Code::NOT_FOUND, "");
  }

  *response = elem->check_response();

  return Status::OK;
}

Status CheckCache::CacheResponse(const std::string& request_signature,
                                 const CheckResponse& response) {
  if (cache_) {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    CheckLRUCache::ScopedLookup lookup(cache_.get(), request_signature);

    int64_t now = SimpleCycleTimer::Now();

    if (lookup.Found()) {
      lookup.value()->set_last_check_time(now);
      lookup.value()->set_check_response(response);
    } else {
      CacheElem* cache_elem = new CacheElem(response, now);
      cache_->Insert(request_signature, cache_elem, 1);
    }
  }

  return Status::OK;
}

// TODO: need to hook up a timer to call Flush.
// Flush aggregated requests whom are longer than flush_interval.
// Called at time specified by GetNextFlushInterval().
Status CheckCache::Flush() {
  if (cache_) {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    cache_->RemoveExpiredEntries();
  }

  return Status::OK;
}

void CheckCache::OnCacheEntryDelete(CacheElem* elem) { delete elem; }

// Flush out aggregated check requests, clear all cache items.
// Usually called at destructor.
Status CheckCache::FlushAll() {
  if (cache_) {
    GOOGLE_LOG(INFO) << "Remove all entries of check cache.";
    std::lock_guard<std::mutex> lock(cache_mutex_);
    cache_->RemoveAll();
  }

  return Status::OK;
}

}  // namespace mixer_client
}  // namespace istio
