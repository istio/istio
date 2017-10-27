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

#include "src/quota_cache.h"
#include "utils/protobuf.h"

using namespace std::chrono;
using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::Attributes_AttributeValue;
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {
namespace {
const std::string kQuotaName = "quota.name";
const std::string kQuotaAmount = "quota.amount";
}

QuotaCache::CacheElem::CacheElem(const std::string& name) : name_(name) {
  prefetch_ = QuotaPrefetch::Create(
      [this](int amount, QuotaPrefetch::DoneFunc fn, QuotaPrefetch::Tick t) {
        Alloc(amount, fn);
      },
      QuotaPrefetch::Options(), system_clock::now());
}

void QuotaCache::CacheElem::Alloc(int amount, QuotaPrefetch::DoneFunc fn) {
  quota_->amount = amount;
  quota_->best_effort = true;
  quota_->response_func = [fn](
      const Attributes&, const CheckResponse::QuotaResult* result) -> bool {
    int amount = -1;
    milliseconds expire = duration_cast<milliseconds>(minutes(1));
    if (result != nullptr) {
      amount = result->granted_amount();
      if (result->has_valid_duration()) {
        expire = ToMilliseonds(result->valid_duration());
      }
    }
    fn(amount, expire, system_clock::now());
    return true;
  };
}

void QuotaCache::CacheElem::Quota(int amount, CheckResult::Quota* quota) {
  quota_ = quota;
  if (prefetch_->Check(amount, system_clock::now())) {
    quota->result = CheckResult::Quota::Passed;
  } else {
    quota->result = CheckResult::Quota::Rejected;
  }

  // A hack that requires prefetch code to call transport Alloc() function
  // within Check() call.
  quota_ = nullptr;
}

QuotaCache::CheckResult::CheckResult() : status_(Code::UNAVAILABLE, "") {}

bool QuotaCache::CheckResult::IsCacheHit() const {
  return status_.error_code() != Code::UNAVAILABLE;
}

bool QuotaCache::CheckResult::BuildRequest(CheckRequest* request) {
  int pending_count = 0;
  std::string rejected_quota_names;
  for (const auto& quota : quotas_) {
    // TODO: return used quota amount to passed quotas.
    if (quota.result == Quota::Rejected) {
      if (!rejected_quota_names.empty()) {
        rejected_quota_names += ",";
      }
      rejected_quota_names += quota.name;
    } else if (quota.result == Quota::Pending) {
      ++pending_count;
    }
    if (quota.response_func) {
      CheckRequest::QuotaParams param;
      param.set_amount(quota.amount);
      param.set_best_effort(quota.best_effort);
      (*request->mutable_quotas())[quota.name] = param;
    }
  }
  if (!rejected_quota_names.empty()) {
    status_ =
        Status(Code::RESOURCE_EXHAUSTED,
               std::string("Quota is exhausted for: ") + rejected_quota_names);
  } else if (pending_count == 0) {
    status_ = Status::OK;
  }
  return request->quotas().size() > 0;
}

void QuotaCache::CheckResult::SetResponse(const Status& status,
                                          const Attributes& attributes,
                                          const CheckResponse& response) {
  std::string rejected_quota_names;
  for (const auto& quota : quotas_) {
    if (quota.response_func) {
      const CheckResponse::QuotaResult* result = nullptr;
      if (status.ok()) {
        const auto& quotas = response.quotas();
        const auto& it = quotas.find(quota.name);
        if (it != quotas.end()) {
          result = &it->second;
        } else {
          GOOGLE_LOG(ERROR) << "Quota response did not have quota for: "
                            << quota.name;
        }
      }
      if (!quota.response_func(attributes, result)) {
        if (!rejected_quota_names.empty()) {
          rejected_quota_names += ",";
        }
        rejected_quota_names += quota.name;
      }
    }
  }
  if (!rejected_quota_names.empty()) {
    status_ =
        Status(Code::RESOURCE_EXHAUSTED,
               std::string("Quota is exhausted for: ") + rejected_quota_names);
  } else {
    status_ = Status::OK;
  }
}

QuotaCache::QuotaCache(const QuotaOptions& options) : options_(options) {
  if (options.num_entries > 0) {
    cache_.reset(new QuotaLRUCache(options.num_entries));
    cache_->SetMaxIdleSeconds(options.expiration_ms / 1000.0);
  }
}

QuotaCache::~QuotaCache() {
  // FlushAll() will remove all cache items.
  FlushAll();
}

void QuotaCache::CheckCache(const Attributes& request, bool check_use_cache,
                            CheckResult::Quota* quota) {
  // If check is not using cache, that check may be rejected.
  // If quota cache is used, quota amount is already substracted from the cache.
  // If the check is rejected, there is not easy way to add them back to cache.
  // The workaround is not to use quota cache if check is not in the cache.
  if (!cache_ || !check_use_cache) {
    quota->best_effort = false;
    quota->result = CheckResult::Quota::Pending;
    quota->response_func = [](
        const Attributes&, const CheckResponse::QuotaResult* result) -> bool {
      // nullptr means connection error, for quota, it is fail open for
      // connection error.
      return result == nullptr || result->granted_amount() > 0;
    };
    return;
  }

  std::lock_guard<std::mutex> lock(cache_mutex_);
  PerQuotaReferenced& quota_ref = quota_referenced_map_[quota->name];
  for (const auto& it : quota_ref.referenced_map) {
    const Referenced& referenced = it.second;
    std::string signature;
    if (!referenced.Signature(request, quota->name, &signature)) {
      continue;
    }
    QuotaLRUCache::ScopedLookup lookup(cache_.get(), signature);
    if (lookup.Found()) {
      CacheElem* cache_elem = lookup.value();
      cache_elem->Quota(quota->amount, quota);
      return;
    }
  }

  if (!quota_ref.pending_item) {
    quota_ref.pending_item.reset(new CacheElem(quota->name));
  }
  quota_ref.pending_item->Quota(quota->amount, quota);

  auto saved_func = quota->response_func;
  std::string quota_name = quota->name;
  quota->response_func = [saved_func, quota_name, this](
      const Attributes& attributes,
      const CheckResponse::QuotaResult* result) -> bool {
    SetResponse(attributes, quota_name, result);
    if (saved_func) {
      return saved_func(attributes, result);
    }
    return true;
  };
}

void QuotaCache::SetResponse(const Attributes& attributes,
                             const std::string& quota_name,
                             const CheckResponse::QuotaResult* result) {
  if (result == nullptr) {
    return;
  }

  Referenced referenced;
  if (!referenced.Fill(result->referenced_attributes())) {
    return;
  }

  std::string signature;
  if (!referenced.Signature(attributes, quota_name, &signature)) {
    GOOGLE_LOG(ERROR) << "Quota response referenced mismatchs with request";
    GOOGLE_LOG(ERROR) << "Request attributes: " << attributes.DebugString();
    GOOGLE_LOG(ERROR) << "Referenced attributes: " << referenced.DebugString();
    return;
  }

  std::lock_guard<std::mutex> lock(cache_mutex_);
  QuotaLRUCache::ScopedLookup lookup(cache_.get(), signature);
  if (lookup.Found()) {
    // Not to override the existing cache entry.
    return;
  }

  PerQuotaReferenced& quota_ref = quota_referenced_map_[quota_name];
  std::string hash = referenced.Hash();
  if (quota_ref.referenced_map.find(hash) == quota_ref.referenced_map.end()) {
    quota_ref.referenced_map[hash] = referenced;
    GOOGLE_LOG(INFO) << "Add a new Referenced for quota cache: " << quota_name
                     << ", reference: " << referenced.DebugString();
  }

  cache_->Insert(signature, quota_ref.pending_item.release(), 1);
}

void QuotaCache::Check(const Attributes& request, bool use_cache,
                       CheckResult* result) {
  // Now, there is only one quota metric for a request.
  // But it should be very easy to support multiple quota metrics.
  static const std::vector<std::pair<std::string, std::string>>
      kQuotaAttributes{{kQuotaName, kQuotaAmount}};
  const auto& attributes_map = request.attributes();
  for (const auto& pair : kQuotaAttributes) {
    const std::string& name_attr = pair.first;
    const std::string& amount_attr = pair.second;
    const auto& name_it = attributes_map.find(name_attr);
    if (name_it == attributes_map.end() ||
        name_it->second.value_case() !=
            Attributes_AttributeValue::kStringValue) {
      continue;
    }
    CheckResult::Quota quota;
    quota.name = name_it->second.string_value();
    quota.amount = 1;
    const auto& amount_it = attributes_map.find(amount_attr);
    if (amount_it != attributes_map.end() &&
        amount_it->second.value_case() ==
            Attributes_AttributeValue::kInt64Value) {
      quota.amount = amount_it->second.int64_value();
    }
    CheckCache(request, use_cache, &quota);
    result->quotas_.push_back(quota);
  }
}

// TODO: hookup with a timer object to call Flush() periodically.
// Be careful; some transport callback functions may be still using
// expired items, need to add ref_count into these callback functions.
Status QuotaCache::Flush() {
  if (cache_) {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    cache_->RemoveExpiredEntries();
  }

  return Status::OK;
}

// Flush out aggregated check requests, clear all cache items.
// Usually called at destructor.
Status QuotaCache::FlushAll() {
  if (cache_) {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    cache_->RemoveAll();
  }

  return Status::OK;
}

}  // namespace mixer_client
}  // namespace istio
