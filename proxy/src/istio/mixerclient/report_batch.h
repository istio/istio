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

#ifndef ISTIO_MIXERCLIENT_REPORT_BATCH_H
#define ISTIO_MIXERCLIENT_REPORT_BATCH_H

#include "proxy/include/istio/mixerclient/client.h"
#include "proxy/src/istio/mixerclient/attribute_compressor.h"

#include <atomic>
#include <mutex>

namespace istio {
namespace mixerclient {

// Report batch, this interface is thread safe.
class ReportBatch {
 public:
  ReportBatch(const ReportOptions& options, TransportReportFunc transport,
              TimerCreateFunc timer_create, AttributeCompressor& compressor);

  virtual ~ReportBatch();

  // Make batched report call.
  void Report(const ::istio::mixer::v1::Attributes& request);

  // Flush out batched reports.
  void Flush();

  uint64_t total_report_calls() const { return total_report_calls_; }
  uint64_t total_remote_report_calls() const {
    return total_remote_report_calls_;
  }

 private:
  void FlushWithLock();

  // The quota options.
  ReportOptions options_;

  // The quota transport
  TransportReportFunc transport_;

  // timer create func
  TimerCreateFunc timer_create_;

  // Attribute compressor.
  AttributeCompressor& compressor_;

  // Mutex guarding the access of batch data;
  std::mutex mutex_;

  // timer to flush out batched data.
  std::unique_ptr<Timer> timer_;

  // batched report compressor
  std::unique_ptr<BatchCompressor> batch_compressor_;

  std::atomic_int_fast64_t total_report_calls_;
  std::atomic_int_fast64_t total_remote_report_calls_;

  GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(ReportBatch);
};

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_REPORT_BATCH_H
