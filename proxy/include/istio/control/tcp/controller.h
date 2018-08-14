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

#ifndef ISTIO_CONTROL_TCP_CONTROLLER_H
#define ISTIO_CONTROL_TCP_CONTROLLER_H

#include "include/istio/control/tcp/request_handler.h"
#include "include/istio/mixerclient/client.h"
#include "mixer/v1/config/client/client_config.pb.h"

namespace istio {
namespace control {
namespace tcp {

// An interface to support Mixer control.
// It takes TcpClientConfig and performs tasks to enforce
// mixer control over TCP requests.
class Controller {
 public:
  virtual ~Controller() {}

  // Creates a TCP request handler.
  // The handler supports making Check and Report calls to Mixer.
  virtual std::unique_ptr<RequestHandler> CreateRequestHandler() = 0;

  // The initial data required by the Controller. It needs:
  // * mixer_config: the mixer client config.
  // * some functions provided by the environment (Envoy)
  struct Options {
    Options(const ::istio::mixer::v1::config::client::TcpClientConfig& config)
        : config(config) {}

    // Mixer filter config
    const ::istio::mixer::v1::config::client::TcpClientConfig& config;

    // Some plaform functions for mixer client library.
    ::istio::mixerclient::Environment env;
  };

  // The factory function to create a new instance of the controller.
  static std::unique_ptr<Controller> Create(const Options& options);

  // Get statistics.
  virtual void GetStatistics(::istio::mixerclient::Statistics* stat) const = 0;
};

}  // namespace tcp
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_TCP_CONTROLLER_H
