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
using ::istio::mixer::v1::CheckRequest;
using ::istio::mixer::v1::CheckResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

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
  quota_->response_func =
      [fn](const CheckResponse::QuotaResult* result) -> bool {
    int amount = -1;
    milliseconds expire;
    if (result != nullptr) {
      amount = result->granted_amount();
      expire = ToMilliseonds(result->valid_duration());
    }
    fn(amount, expire, system_clock::now());
    return true;
  };
}

bool QuotaCache::CacheElem::Quota(int amount, CheckResult::Quota* quota) {
  quota_ = quota;
  bool ret = prefetch_->Check(amount, system_clock::now());
  // A hack that requires prefetch code to call transport Alloc() function
  // within Check() call.
  quota_ = nullptr;
  return ret;
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
      if (!quota.response_func(result)) {
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

void QuotaCache::CheckCache(const Attributes& request, bool use_cache,
                            CheckResult::Quota* quota) {
  if (!cache_ || !use_cache) {
    quota->best_effort = false;
    quota->result = CheckResult::Quota::Pending;
    quota->response_func =
        [](const CheckResponse::QuotaResult* result) -> bool {
      // nullptr means connection error, for quota, it is fail open for
      // connection error.
      return result == nullptr || result->granted_amount() > 0;
    };
    return;
  }

  // TODO: for now, quota cache key always is the quota name.
  // Need to use ReferencedAttributes from Mixer server to calculate cache key.
  std::string signature = quota->name;

  std::lock_guard<std::mutex> lock(cache_mutex_);
  QuotaLRUCache::ScopedLookup lookup(cache_.get(), signature);
  CacheElem* cache_elem;
  if (!lookup.Found()) {
    cache_elem = new CacheElem(quota->name);
    cache_->Insert(signature, cache_elem, 1);
  } else {
    cache_elem = lookup.value();
  }

  if (cache_elem->Quota(quota->amount, quota)) {
    quota->result = CheckResult::Quota::Passed;
  } else {
    // TODO: for multiple quota metrics in a request,
    // if a metric is rejected, other metrics should not use any tokens.
    // One way is for prefetch to implement TryQuota, we call TryQuota
    // for all metrics first, only all passed, then deduce the tokens.
    quota->result = CheckResult::Quota::Rejected;
  }
}

void QuotaCache::Check(const Attributes& request, bool use_cache,
                       CheckResult* result) {
  // Now, there is only one quota metric for a request.
  // But it should be very easy to support multiple quota metrics.
  static const std::vector<std::pair<std::string, std::string>>
      kQuotaAttributes{{Attributes::kQuotaName, Attributes::kQuotaAmount}};
  for (const auto& pair : kQuotaAttributes) {
    const std::string& name_attr = pair.first;
    const std::string& amount_attr = pair.second;
    const auto& name_it = request.attributes.find(name_attr);
    if (name_it == request.attributes.end() ||
        name_it->second.type != Attributes::Value::STRING) {
      continue;
    }
    CheckResult::Quota quota;
    quota.name = name_it->second.str_v;
    quota.amount = 1;
    const auto& amount_it = request.attributes.find(amount_attr);
    if (amount_it != request.attributes.end() &&
        amount_it->second.type == Attributes::Value::INT64) {
      quota.amount = amount_it->second.value.int64_v;
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
