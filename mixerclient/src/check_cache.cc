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
#include "utils/protobuf.h"

using namespace std::chrono;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

void CheckCache::CacheElem::CacheElem::SetResponse(
    const CheckResponse& response, Tick time_now) {
  if (response.has_precondition()) {
    status_ = parent_.ConvertRpcStatus(response.precondition().status());

    if (response.precondition().has_valid_duration()) {
      expire_time_ =
          time_now + ToMilliseonds(response.precondition().valid_duration());
    } else {
      // never expired.
      expire_time_ = time_point<system_clock>::max();
    }
    use_count_ = response.precondition().valid_use_count();
  } else {
    status_ = Status(Code::INVALID_ARGUMENT,
                     "CheckResponse doesn't have PreconditionResult");
    use_count_ = 0;           // 0 for not used this cache.
    expire_time_ = time_now;  // expired now.
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

CheckCache::CheckResult::CheckResult() : status_(Code::UNAVAILABLE, "") {}

bool CheckCache::CheckResult::IsCacheHit() const {
  return status_.error_code() != Code::UNAVAILABLE;
}

CheckCache::CheckCache(const CheckOptions& options) : options_(options) {
  if (options.num_entries > 0) {
    cache_.reset(new CheckLRUCache(options.num_entries));
  }
}

CheckCache::~CheckCache() {
  // FlushAll() will remove all cache items.
  FlushAll();
}

void CheckCache::Check(const Attributes& attributes, CheckResult* result) {
  Status status = Check(attributes, system_clock::now());
  if (status.error_code() != Code::NOT_FOUND) {
    result->status_ = status;
  }

  result->on_response_ = [this](const Status& status,
                                const Attributes& attributes,
                                const CheckResponse& response) -> Status {
    if (!status.ok()) {
      if (options_.network_fail_open) {
        return Status::OK;
      } else {
        return status;
      }
    } else {
      return CacheResponse(attributes, response, system_clock::now());
    }
  };
}

Status CheckCache::Check(const Attributes& attributes, Tick time_now) {
  if (!cache_) {
    // By returning NOT_FOUND, caller will send request to server.
    return Status(Code::NOT_FOUND, "");
  }

  for (const auto& it : referenced_map_) {
    const Referenced& reference = it.second;
    std::string signature;
    if (!reference.Signature(attributes, "", &signature)) {
      continue;
    }

    std::lock_guard<std::mutex> lock(cache_mutex_);
    CheckLRUCache::ScopedLookup lookup(cache_.get(), signature);
    if (lookup.Found()) {
      CacheElem* elem = lookup.value();
      if (elem->IsExpired(time_now)) {
        cache_->Remove(signature);
        return Status(Code::NOT_FOUND, "");
      }
      return elem->status();
    }
  }

  return Status(Code::NOT_FOUND, "");
}

Status CheckCache::CacheResponse(const Attributes& attributes,
                                 const CheckResponse& response, Tick time_now) {
  if (!cache_ || !response.has_precondition()) {
    if (response.has_precondition()) {
      return ConvertRpcStatus(response.precondition().status());
    } else {
      return Status(Code::INVALID_ARGUMENT,
                    "CheckResponse doesn't have PreconditionResult");
    }
  }

  Referenced referenced;
  if (!referenced.Fill(response.precondition().referenced_attributes())) {
    // Failed to decode referenced_attributes, not to cache this result.
    return ConvertRpcStatus(response.precondition().status());
  }
  std::string signature;
  if (!referenced.Signature(attributes, "", &signature)) {
    GOOGLE_LOG(ERROR) << "Response referenced mismatchs with request";
    GOOGLE_LOG(ERROR) << "Request attributes: " << attributes.DebugString();
    GOOGLE_LOG(ERROR) << "Referenced attributes: " << referenced.DebugString();
    return ConvertRpcStatus(response.precondition().status());
  }

  std::lock_guard<std::mutex> lock(cache_mutex_);
  std::string hash = referenced.Hash();
  if (referenced_map_.find(hash) == referenced_map_.end()) {
    referenced_map_[hash] = referenced;
    GOOGLE_LOG(INFO) << "Add a new Referenced for check cache: "
                     << referenced.DebugString();
  }

  CheckLRUCache::ScopedLookup lookup(cache_.get(), signature);
  if (lookup.Found()) {
    lookup.value()->SetResponse(response, time_now);
    return lookup.value()->status();
  }

  CacheElem* cache_elem = new CacheElem(*this, response, time_now);
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

Status CheckCache::ConvertRpcStatus(const ::google::rpc::Status& status) const {
  // If server status code is INTERNAL, check network_fail_open flag.
  if (status.code() == Code::INTERNAL && options_.network_fail_open) {
    return Status::OK;
  } else {
    return Status(static_cast<Code>(status.code()), status.message());
  }
}

}  // namespace mixer_client
}  // namespace istio
