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

#ifndef MIXERCONTROL_HTTP_REQUEST_HANDLER_H
#define MIXERCONTROL_HTTP_REQUEST_HANDLER_H

#include "check_data.h"
#include "include/client.h"
#include "report_data.h"

namespace istio {
namespace mixer_control {
namespace http {

// The interface to handle a HTTP request.
class RequestHandler {
 public:
  virtual ~RequestHandler() {}

  // Perform a Check call. It will:
  // * extract forwarded attributes from client proxy
  // * extract attributes from the request
  // * extract attributes from the config.
  // * if necessary, forward some attributes to downstream
  // * make a Check call.
  virtual ::istio::mixer_client::CancelFunc Check(
      CheckData* check_data,
      ::istio::mixer_client::TransportCheckFunc transport,
      ::istio::mixer_client::DoneFunc on_done) = 0;

  // Make a Report call. It will:
  // * check service config to see if Report is required
  // * extract more report attributes
  // * make a Report call.
  virtual void Report(ReportData* report_data) = 0;
};

}  // namespace http
}  // namespace mixer_control
}  // namespace istio

#endif  // MIXERCONTROL_HTTP_REQUEST_HANDLER_H
