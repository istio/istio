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

#ifndef ISTIO_CONTROL_HTTP_REQUEST_HANDLER_IMPL_H
#define ISTIO_CONTROL_HTTP_REQUEST_HANDLER_IMPL_H

#include "proxy/include/istio/control/http/request_handler.h"
#include "proxy/src/istio/control/http/client_context.h"
#include "proxy/src/istio/control/http/service_context.h"
#include "proxy/src/istio/control/request_context.h"

namespace istio {
namespace control {
namespace http {

// The class to implement HTTPRequestHandler interface.
class RequestHandlerImpl : public RequestHandler {
 public:
  RequestHandlerImpl(std::shared_ptr<ServiceContext> service_context);

  // Makes a Check call.
  ::istio::mixerclient::CancelFunc Check(
      CheckData* check_data, HeaderUpdate* header_update,
      ::istio::mixerclient::TransportCheckFunc transport,
      ::istio::mixerclient::CheckDoneFunc on_done) override;

  // Make a Report call.
  void Report(ReportData* report_data) override;

  void ExtractRequestAttributes(CheckData* check_data) override;

 private:
  // The request context object.
  RequestContext request_context_;

  // The service context.
  std::shared_ptr<ServiceContext> service_context_;
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_REQUEST_HANDLER_IMPL_H
