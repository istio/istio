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

#ifndef MIXERCLIENT_CLIENT_IMPL_H
#define MIXERCLIENT_CLIENT_IMPL_H

#include "include/client.h"
#include "src/attribute_converter.h"
#include "src/check_cache.h"
#include "src/quota_cache.h"
#include "src/report_batch.h"

#include <atomic>

namespace istio {
namespace mixer_client {

class MixerClientImpl : public MixerClient {
 public:
  // Constructor
  MixerClientImpl(const MixerClientOptions& options);

  // Destructor
  virtual ~MixerClientImpl();

  virtual CancelFunc Check(const Attributes& attributes,
                           TransportCheckFunc transport, DoneFunc on_done);
  virtual void Report(const Attributes& attributes);

 private:
  // Store the options
  MixerClientOptions options_;

  // To convert attributes into protobuf
  AttributeConverter converter_;

  // Cache for Check call.
  std::unique_ptr<CheckCache> check_cache_;
  // Report batch.
  std::unique_ptr<ReportBatch> report_batch_;
  // Cache for Quota call.
  std::unique_ptr<QuotaCache> quota_cache_;

  // for deduplication_id
  std::string deduplication_id_base_;
  std::atomic<std::uint64_t> deduplication_id_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(MixerClientImpl);
};

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_CLIENT_IMPL_H
