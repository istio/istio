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

#ifndef ISTIO_MIXERCLIENT_CLIENT_IMPL_H
#define ISTIO_MIXERCLIENT_CLIENT_IMPL_H

#include "include/istio/mixerclient/client.h"
#include "src/istio/mixerclient/attribute_compressor.h"
#include "src/istio/mixerclient/check_cache.h"
#include "src/istio/mixerclient/quota_cache.h"
#include "src/istio/mixerclient/report_batch.h"

#include <atomic>

namespace istio {
namespace mixerclient {

class MixerClientImpl : public MixerClient {
 public:
  // Constructor
  MixerClientImpl(const MixerClientOptions& options);

  // Destructor
  virtual ~MixerClientImpl();

  CancelFunc Check(
      const ::istio::mixer::v1::Attributes& attributes,
      const std::vector<::istio::quota_config::Requirement>& quotas,
      TransportCheckFunc transport, CheckDoneFunc on_done) override;
  void Report(const ::istio::mixer::v1::Attributes& attributes) override;

  void GetStatistics(Statistics* stat) const override;

 private:
  // Store the options
  MixerClientOptions options_;

  // To compress attributes.
  AttributeCompressor compressor_;

  // Cache for Check call.
  std::unique_ptr<CheckCache> check_cache_;
  // Report batch.
  std::unique_ptr<ReportBatch> report_batch_;
  // Cache for Quota call.
  std::unique_ptr<QuotaCache> quota_cache_;

  // for deduplication_id
  std::string deduplication_id_base_;
  std::atomic<std::uint64_t> deduplication_id_;

  // Atomic objects for recording statistics.
  // check cache miss rate:
  // total_blocking_remote_check_calls_ / total_check_calls_.
  // quota cache miss rate:
  // total_blocking_remote_quota_calls_ / total_quota_calls_.
  std::atomic_int_fast64_t total_check_calls_;
  std::atomic_int_fast64_t total_remote_check_calls_;
  std::atomic_int_fast64_t total_blocking_remote_check_calls_;
  std::atomic_int_fast64_t total_quota_calls_;
  std::atomic_int_fast64_t total_remote_quota_calls_;
  std::atomic_int_fast64_t total_blocking_remote_quota_calls_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(MixerClientImpl);
};

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_CLIENT_IMPL_H
