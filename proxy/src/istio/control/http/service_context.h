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

#ifndef ISTIO_CONTROL_HTTP_SERVICE_CONTEXT_H
#define ISTIO_CONTROL_HTTP_SERVICE_CONTEXT_H

#include "google/protobuf/stubs/status.h"
#include "mixer/v1/attributes.pb.h"
#include "proxy/include/istio/api_spec/http_api_spec_parser.h"
#include "proxy/include/istio/quota_config/config_parser.h"
#include "proxy/src/istio/control/http/client_context.h"

namespace istio {
namespace control {
namespace http {

// The context to hold service config for both HTTP and TCP.
class ServiceContext {
 public:
  ServiceContext(
      std::shared_ptr<ClientContext> client_context,
      const ::istio::mixer::v1::config::client::ServiceConfig* config);

  std::shared_ptr<ClientContext> client_context() const {
    return client_context_;
  }

  // Add static mixer attributes.
  void AddStaticAttributes(RequestContext* request) const;

  // Inject a header that contains the static forwarded attributes.
  void InjectForwardedAttributes(HeaderUpdate* header_update) const;

  // Add api attributes from api_spec.
  void AddApiAttributes(CheckData* check_data, RequestContext* request) const;

  // Add quota requirements from quota configs.
  void AddQuotas(RequestContext* request) const;

  bool enable_mixer_check() const {
    return service_config_ && !service_config_->disable_check_calls();
  }
  bool enable_mixer_report() const {
    return service_config_ && !service_config_->disable_report_calls();
  }

 private:
  // Pre-process the config data to build parser objects.
  void BuildParsers();

  // The client context object.
  std::shared_ptr<ClientContext> client_context_;

  // Concatenated api_spec_
  ::istio::mixer::v1::config::client::HTTPAPISpec api_spec_;
  // Api spec parser to generate api attributes and api_key
  std::unique_ptr<::istio::api_spec::HttpApiSpecParser> api_spec_parser_;

  // The quota parsers for each quota config.
  std::vector<std::unique_ptr<::istio::quota_config::ConfigParser>>
      quota_parsers_;

  // The service config.
  std::unique_ptr<::istio::mixer::v1::config::client::ServiceConfig>
      service_config_;
};

}  // namespace http
}  // namespace control
}  // namespace istio

#endif  // ISTIO_CONTROL_HTTP_SERVICE_CONTEXT_H
