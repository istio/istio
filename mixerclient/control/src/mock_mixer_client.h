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

#ifndef MIXERCONTROL_MOCK_MIXER_CLIENT_H
#define MIXERCONTROL_MOCK_MIXER_CLIENT_H

#include "gmock/gmock.h"
#include "include/client.h"

namespace istio {
namespace mixer_control {

// The mock object for MixerClient interface.
class MockMixerClient : public ::istio::mixer_client::MixerClient {
 public:
  MOCK_METHOD3(Check, ::istio::mixer_client::CancelFunc(
                          const ::istio::mixer::v1::Attributes& attributes,
                          ::istio::mixer_client::TransportCheckFunc transport,
                          ::istio::mixer_client::DoneFunc on_done));
  MOCK_METHOD1(Report, void(const ::istio::mixer::v1::Attributes& attributes));
};

}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_MOCK_MIXER_CLIENT_H
