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
#include "src/signature.h"
#include "utils/protobuf.h"

using namespace std::chrono;
using ::istio::mixer::v1::QuotaRequest;
using ::istio::mixer::v1::QuotaResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

QuotaCache::CacheElem::CacheElem(const QuotaRequest& request,
                                 TransportQuotaFunc transport)
    : request_(request), transport_(transport) {
  prefetch_ = QuotaPrefetch::Create(
      [this](int amount, QuotaPrefetch::DoneFunc fn, QuotaPrefetch::Tick t) {
        Alloc(amount, fn);
      },
      QuotaPrefetch::Options(), system_clock::now());
}

void QuotaCache::CacheElem::Alloc(int amount, QuotaPrefetch::DoneFunc fn) {
  auto response = new QuotaResponse;
  request_.set_amount(amount);
  transport_(request_, response, [response, fn](const Status& status) {
    int amount = -1;
    milliseconds expire;
    if (status.ok()) {
      amount = response->amount();
      expire = ToMilliseonds(response->expiration());
    }
    delete response;
    fn(amount, expire, system_clock::now());
  });
}

bool QuotaCache::CacheElem::Quota(const Attributes& request) {
  int amount = 1;
  const auto& it = request.attributes.find(Attributes::kQuotaAmount);
  if (it != request.attributes.end()) {
    amount = it->second.value.int64_v;
  }
  return prefetch_->Check(amount, system_clock::now());
}

QuotaCache::QuotaCache(const QuotaOptions& options,
                       TransportQuotaFunc transport,
                       const AttributeConverter& converter)
    : options_(options),
      transport_(transport),
      converter_(converter),
      deduplication_id_(0) {
  if (options.num_entries > 0) {
    cache_.reset(new QuotaLRUCache(
        options.num_entries, std::bind(&QuotaCache::OnCacheEntryDelete, this,
                                       std::placeholders::_1)));
    cache_->SetMaxIdleSeconds(options.expiration_ms / 1000.0);

    // Excluse quota_amount in the key calculation.
    cache_keys_ = CacheKeySet::CreateExclusive({Attributes::kQuotaAmount});
  }
}

QuotaCache::~QuotaCache() {
  // FlushAll() will remove all cache items.
  FlushAll();
}

void QuotaCache::Quota(const Attributes& request, DoneFunc on_done) {
  // Makes sure quota_name is provided and with correct type.
  const auto& it = request.attributes.find(Attributes::kQuotaName);
  if (it == request.attributes.end() ||
      it->second.type != Attributes::Value::STRING) {
    on_done(Status(Code::INVALID_ARGUMENT,
                   std::string("A required attribute is missing: ") +
                       Attributes::kQuotaName));
    return;
  }

  if (!cache_) {
    QuotaRequest pb_request;
    Convert(request, false, &pb_request);
    auto response = new QuotaResponse;
    transport_(pb_request, response, [response, on_done](const Status& status) {
      delete response;
      // If code is UNAVAILABLE, it is network error.
      // fail open for network error.
      if (status.error_code() == Code::UNAVAILABLE) {
        on_done(Status::OK);
      } else {
        on_done(status);
      }
    });
    return;
  }

  std::string signature = GenerateSignature(request, *cache_keys_);

  std::lock_guard<std::mutex> lock(cache_mutex_);
  QuotaLRUCache::ScopedLookup lookup(cache_.get(), signature);

  CacheElem* cache_elem;
  if (!lookup.Found()) {
    QuotaRequest pb_request;
    Convert(request, true, &pb_request);
    cache_elem = new CacheElem(pb_request, transport_);
    cache_->Insert(signature, cache_elem, 1);
  } else {
    cache_elem = lookup.value();
  }

  if (cache_elem->Quota(request)) {
    on_done(Status::OK);
  } else {
    on_done(Status(
        Code::RESOURCE_EXHAUSTED,
        std::string("Quota is exhausted for: ") + cache_elem->quota_name()));
  }
}

void QuotaCache::Convert(const Attributes& attributes, bool best_effort,
                         QuotaRequest* request) {
  Attributes filtered_attributes;
  bool amount_set = false;
  for (const auto& it : attributes.attributes) {
    if (it.first == Attributes::kQuotaName &&
        it.second.type == Attributes::Value::STRING) {
      request->set_quota(it.second.str_v);
    } else if (it.first == Attributes::kQuotaAmount &&
               it.second.type == Attributes::Value::INT64) {
      request->set_amount(it.second.value.int64_v);
      amount_set = true;
    } else {
      filtered_attributes.attributes[it.first] = it.second;
    }
  }
  if (!amount_set) {
    // If amount is not set, default to 1.
    request->set_amount(1);
  }
  request->set_deduplication_id(std::to_string(deduplication_id_++));
  request->set_best_effort(best_effort);
  converter_.Convert(filtered_attributes, request->mutable_attributes());
}

// TODO: hookup with a timer object to call Flush() periodically.
Status QuotaCache::Flush() {
  if (cache_) {
    std::lock_guard<std::mutex> lock(cache_mutex_);
    cache_->RemoveExpiredEntries();
  }

  return Status::OK;
}

void QuotaCache::OnCacheEntryDelete(CacheElem* elem) { delete elem; }

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
