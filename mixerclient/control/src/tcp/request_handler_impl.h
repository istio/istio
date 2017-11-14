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

#ifndef MIXERCONTROL_TCP_REQUEST_HANDLER_IMPL_H
#define MIXERCONTROL_TCP_REQUEST_HANDLER_IMPL_H

#include "client_context.h"
#include "control/include/tcp/request_handler.h"
#include "control/src/request_context.h"

namespace istio {
namespace mixer_control {
namespace tcp {

// The class to implement RequestHandler interface.
class RequestHandlerImpl : public RequestHandler {
 public:
  RequestHandlerImpl(std::shared_ptr<ClientContext> client_context);

  // Make a Check call.
  ::istio::mixer_client::CancelFunc Check(
      CheckData* check_data, ::istio::mixer_client::DoneFunc on_done) override;

  // Make a Report call.
  void Report(ReportData* report_data) override;

 private:
  // The request context object.
  RequestContext request_context_;

  // The client context object.
  std::shared_ptr<ClientContext> client_context_;
};

}  // namespace tcp
}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_TCP_REQUEST_HANDLER_IMPL_H
