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

#ifndef ISTIO_CONTROL_MOCK_MIXER_CLIENT_H
#define ISTIO_CONTROL_MOCK_MIXER_CLIENT_H

#include "gmock/gmock.h"
#include "proxy/include/istio/mixerclient/client.h"

namespace istio {
namespace control {

// The mock object for MixerClient interface.
class MockMixerClient : public ::istio::mixerclient::MixerClient {
 public:
  MOCK_METHOD4(
      Check, ::istio::mixerclient::CancelFunc(
                 const ::istio::mixer::v1::Attributes& attributes,
                 const std::vector<::istio::quota_config::Requirement>& quotas,
                 ::istio::mixerclient::TransportCheckFunc transport,
                 ::istio::mixerclient::CheckDoneFunc on_done));
  MOCK_METHOD1(Report, void(const ::istio::mixer::v1::Attributes& attributes));
  MOCK_CONST_METHOD1(GetStatistics,
                     void(::istio::mixerclient::Statistics* stat));
};

}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_MOCK_MIXER_CLIENT_H
