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

#pragma once

#include "mixer/v1/config/client/client_config.pb.h"

namespace Envoy {
namespace Http {
namespace Mixer {

// Config for http filter.
class Config {
 public:
  Config(const ::istio::mixer::v1::config::client::HttpClientConfig& config_pb);

  // The Http client config.
  const ::istio::mixer::v1::config::client::HttpClientConfig& config_pb()
      const {
    return config_pb_;
  }

  // check cluster
  const std::string& check_cluster() const {
    return config_pb_.transport().check_cluster();
  }
  // report cluster
  const std::string& report_cluster() const {
    return config_pb_.transport().report_cluster();
  }

 private:
  // The Http client config.
  ::istio::mixer::v1::config::client::HttpClientConfig config_pb_;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
