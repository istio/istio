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

#include "src/report_batch.h"
#include "utils/protobuf.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::ReportRequest;
using ::istio::mixer::v1::ReportResponse;
using ::google::protobuf::util::Status;
using ::google::protobuf::util::error::Code;

namespace istio {
namespace mixer_client {

ReportBatch::ReportBatch(const ReportOptions& options,
                         TransportReportFunc transport,
                         TimerCreateFunc timer_create,
                         AttributeCompressor& compressor)
    : options_(options),
      transport_(transport),
      timer_create_(timer_create),
      compressor_(compressor) {}

ReportBatch::~ReportBatch() { Flush(); }

void ReportBatch::Report(const Attributes& request) {
  std::lock_guard<std::mutex> lock(mutex_);
  if (!batch_compressor_) {
    batch_compressor_ = compressor_.CreateBatchCompressor();
  }

  if (!batch_compressor_->Add(request)) {
    FlushWithLock();

    batch_compressor_ = compressor_.CreateBatchCompressor();
    batch_compressor_->Add(request);
  }

  if (batch_compressor_->size() >= options_.max_batch_entries) {
    FlushWithLock();
  } else {
    if (batch_compressor_->size() == 1 && timer_create_) {
      if (!timer_) {
        timer_ = timer_create_([this]() { Flush(); });
      }
      timer_->Start(options_.max_batch_time_ms);
    }
  }
}

void ReportBatch::FlushWithLock() {
  if (!batch_compressor_) {
    return;
  }

  std::unique_ptr<ReportRequest> request = batch_compressor_->Finish();
  batch_compressor_.reset();
  if (timer_) {
    timer_->Stop();
  }

  ReportResponse* response = new ReportResponse;
  transport_(*request, response, [this, response](const Status& status) {
    delete response;
    if (!status.ok()) {
      GOOGLE_LOG(ERROR) << "Mixer Report failed with: " << status.ToString();
      if (InvalidDictionaryStatus(status)) {
        compressor_.ShrinkGlobalDictionary();
      }
    }
  });
}

void ReportBatch::Flush() {
  std::lock_guard<std::mutex> lock(mutex_);
  FlushWithLock();
}

}  // namespace mixer_client
}  // namespace istio
