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

#ifndef ISTIO_CONTROL_TCP_CONTROLLER_IMPL_H
#define ISTIO_CONTROL_TCP_CONTROLLER_IMPL_H

#include <memory>

#include "include/istio/control/tcp/controller.h"
#include "src/istio/control/tcp/client_context.h"

namespace istio {
namespace control {
namespace tcp {

// The class to implement Controller interface.
class ControllerImpl : public Controller {
 public:
  ControllerImpl(const Controller::Options& data);
  // A constructor for unit-test to pass in a mock client_context
  ControllerImpl(std::shared_ptr<ClientContext> client_context)
      : client_context_(client_context) {}

  // Creates a TCP request handler
  std::unique_ptr<RequestHandler> CreateRequestHandler() override;

  // Get statistics.
  void GetStatistics(::istio::mixerclient::Statistics* stat) const override;

 private:
  // The client context object to hold client config and client cache.
  std::shared_ptr<ClientContext> client_context_;
};

}  // namespace tcp
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_TCP_CONTROLLER_IMPL_H
