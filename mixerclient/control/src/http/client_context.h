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

#ifndef MIXERCONTROL_HTTP_CLIENT_CONTEXT_H
#define MIXERCONTROL_HTTP_CLIENT_CONTEXT_H

#include "control/include/http/controller.h"
#include "control/src/client_context_base.h"

namespace istio {
namespace mixer_control {
namespace http {

// The global context object to hold:
// * the mixer client config
// * the mixer client object to call Check/Report with cache.
class ClientContext : public ClientContextBase {
 public:
  ClientContext(const Controller::Options& data)
      : ClientContextBase(data.config.transport(), data.env),
        config_(data.config) {}

  // A constructor for unit-test to pass in a mock mixer_client
  ClientContext(
      std::unique_ptr<::istio::mixer_client::MixerClient> mixer_client,
      const ::istio::mixer::v1::config::client::HttpClientConfig& config)
      : ClientContextBase(std::move(mixer_client)), config_(config) {}

  // Retrieve mixer client config.
  const ::istio::mixer::v1::config::client::HttpClientConfig& config() const {
    return config_;
  }

 private:
  // The http client config.
  const ::istio::mixer::v1::config::client::HttpClientConfig& config_;
};

}  // namespace http
}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_HTTP_CLIENT_CONTEXT_H
