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

#ifndef MIXERCLIENT_CLIENT_H
#define MIXERCLIENT_CLIENT_H

#include "google/protobuf/stubs/status.h"
#include "mixer/v1/service.pb.h"
#include "options.h"
#include "timer.h"

namespace istio {
namespace mixer_client {

// Defines a function prototype used when an asynchronous transport call
// is completed.
// Uses UNAVAILABLE status code to indicate network failure.
using DoneFunc = std::function<void(const ::google::protobuf::util::Status&)>;

// Defines a function prototype used to cancel an asynchronous transport call.
using CancelFunc = std::function<void()>;

// Defines a function prototype to make an asynchronous Check call
using TransportCheckFunc = std::function<CancelFunc(
    const ::istio::mixer::v1::CheckRequest& request,
    ::istio::mixer::v1::CheckResponse* response, DoneFunc on_done)>;

// Defines a function prototype to make an asynchronous Report call
using TransportReportFunc = std::function<CancelFunc(
    const ::istio::mixer::v1::ReportRequest& request,
    ::istio::mixer::v1::ReportResponse* response, DoneFunc on_done)>;

// Defines a function prototype to generate an UUID
using UUIDGenerateFunc = std::function<std::string()>;

// Defines the options to create an instance of MixerClient interface.
struct MixerClientOptions {
  // Default constructor with default values.
  MixerClientOptions() {}

  // Constructor with specified option values.
  MixerClientOptions(const CheckOptions& check_options,
                     const ReportOptions& report_options,
                     const QuotaOptions& quota_options)
      : check_options(check_options),
        report_options(report_options),
        quota_options(quota_options) {}

  // Check options.
  CheckOptions check_options;
  // Report options.
  ReportOptions report_options;
  // Quota options.
  QuotaOptions quota_options;

  // Transport functions.
  TransportCheckFunc check_transport;
  TransportReportFunc report_transport;

  // Timer create function.
  // Usually there are some restrictions on timer_create_func.
  // Don't call it at program start, or init time, it is not ready.
  // It is safe to call during Check() or Report() calls.
  TimerCreateFunc timer_create_func;

  // UUID generating function
  UUIDGenerateFunc uuid_generate_func;
};

class MixerClient {
 public:
  // Destructor
  virtual ~MixerClient() {}

  // Attribute based calls will be used.
  // Callers should pass in the full set of attributes for the call.
  // The client will use the full set attributes to check cache. If cache
  // miss, an attribute context based on the underlying gRPC stream will
  // be used to generate attribute_update and send that to Mixer server.
  // Callers don't need response data, they only need success or failure.
  // The response data from mixer will be consumed by mixer client.

  // A check call.
  virtual CancelFunc Check(const ::istio::mixer::v1::Attributes& attributes,
                           TransportCheckFunc transport, DoneFunc on_done) = 0;

  // A report call.
  virtual void Report(const ::istio::mixer::v1::Attributes& attributes) = 0;
};

// Creates a MixerClient object.
std::unique_ptr<MixerClient> CreateMixerClient(
    const MixerClientOptions& options);

}  // namespace mixer_client
}  // namespace istio

#endif  // MIXERCLIENT_CLIENT_H
