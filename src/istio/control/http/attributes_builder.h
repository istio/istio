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

#ifndef ISTIO_CONTROL_HTTP_ATTRIBUTES_BUILDER_H
#define ISTIO_CONTROL_HTTP_ATTRIBUTES_BUILDER_H

#include "include/istio/control/http/check_data.h"
#include "include/istio/control/http/report_data.h"
#include "src/istio/control/request_context.h"

namespace istio {
namespace control {
namespace http {

// The context for each HTTP request.
class AttributesBuilder {
 public:
  AttributesBuilder(RequestContext* request) : request_(request) {}

  // Extract forwarded attributes from HTTP header.
  void ExtractForwardedAttributes(CheckData* check_data);
  // Forward attributes to upstream proxy.
  static void ForwardAttributes(
      const ::istio::mixer::v1::Attributes& attributes,
      HeaderUpdate* header_update);

  // Extract attributes for Check call.
  void ExtractCheckAttributes(CheckData* check_data);
  // Extract attributes for Report call.
  void ExtractReportAttributes(ReportData* report_data);

 private:
  // Extract HTTP header attributes
  void ExtractRequestHeaderAttributes(CheckData* check_data);
  // Extract authentication attributes for Check call. Going forward, this
  // function will use authentication result (from authn filter), which will set
  // all authenticated attributes (including source_user, request.auth.*).
  // During the transition (i.e authn filter is not added to sidecar), this
  // function will also look up the (jwt) payload when authentication result is
  // not available.
  void ExtractAuthAttributes(CheckData* check_data);

  // The request context object.
  RequestContext* request_;
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_ATTRIBUTES_BUILDER_H
