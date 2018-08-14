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

#ifndef ISTIO_MIXERCLIENT_CLIENT_H
#define ISTIO_MIXERCLIENT_CLIENT_H

#include "environment.h"
#include "proxy/include/istio/quota_config/requirement.h"
#include "options.h"

#include <vector>

namespace istio {
namespace mixerclient {

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
  // The environment functions.
  Environment env;
};

// The statistics recorded by mixerclient library.
struct Statistics {
  // Total number of check calls.
  uint64_t total_check_calls;
  // Total number of remote check calls.
  uint64_t total_remote_check_calls;
  // Total number of remote check calls that blocking origin requests.
  uint64_t total_blocking_remote_check_calls;

  // Total number of quota calls.
  uint64_t total_quota_calls;
  // Total number of remote quota calls.
  uint64_t total_remote_quota_calls;
  // Total number of remote quota calls that blocking origin requests.
  uint64_t total_blocking_remote_quota_calls;

  // Total number of report calls.
  uint64_t total_report_calls;
  // Total number of remote report calls.
  uint64_t total_remote_report_calls;
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
  virtual CancelFunc Check(
      const ::istio::mixer::v1::Attributes& attributes,
      const std::vector<::istio::quota_config::Requirement>& quotas,
      TransportCheckFunc transport, CheckDoneFunc on_done) = 0;

  // A report call.
  virtual void Report(const ::istio::mixer::v1::Attributes& attributes) = 0;

  // Get statistics.
  virtual void GetStatistics(Statistics* stat) const = 0;
};

// Creates a MixerClient object.
std::unique_ptr<MixerClient> CreateMixerClient(
    const MixerClientOptions& options);

}  // namespace mixerclient
}  // namespace istio

#endif  // ISTIO_MIXERCLIENT_CLIENT_H
