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

#include "service_context.h"
#include "include/istio/utils/attribute_names.h"
#include "src/istio/control/http/attributes_builder.h"

using ::istio::mixer::v1::Attributes;
using ::istio::mixer::v1::config::client::ServiceConfig;

namespace istio {
namespace control {
namespace http {

ServiceContext::ServiceContext(std::shared_ptr<ClientContext> client_context,
                               const ServiceConfig *config)
    : client_context_(client_context) {
  if (config) {
    service_config_.reset(new ServiceConfig(*config));
  }
  BuildParsers();
}

void ServiceContext::BuildParsers() {
  if (!service_config_) {
    return;
  }
  // Build api_spec parsers
  for (const auto &api_spec : service_config_->http_api_spec()) {
    api_spec_.MergeFrom(api_spec);
  }
  api_spec_parser_ = ::istio::api_spec::HttpApiSpecParser::Create(api_spec_);

  // Build quota parser
  for (const auto &quota : service_config_->quota_spec()) {
    quota_parsers_.push_back(
        ::istio::quota_config::ConfigParser::Create(quota));
  }
}

// Add static mixer attributes.
void ServiceContext::AddStaticAttributes(RequestContext *request) const {
  if (client_context_->config().has_mixer_attributes()) {
    request->attributes.MergeFrom(client_context_->config().mixer_attributes());
  }
  if (service_config_ && service_config_->has_mixer_attributes()) {
    request->attributes.MergeFrom(service_config_->mixer_attributes());
  }
}

// Inject a header that contains the static forwarded attributes.
void ServiceContext::InjectForwardedAttributes(
    HeaderUpdate *header_update) const {
  Attributes attributes;

  if (client_context_->config().has_forward_attributes()) {
    attributes.MergeFrom(client_context_->config().forward_attributes());
  }
  if (service_config_ && service_config_->has_forward_attributes()) {
    attributes.MergeFrom(service_config_->forward_attributes());
  }

  if (!attributes.attributes().empty()) {
    AttributesBuilder::ForwardAttributes(attributes, header_update);
  }
}

void ServiceContext::AddApiAttributes(CheckData *check_data,
                                      RequestContext *request) const {
  if (!api_spec_parser_) {
    return;
  }
  std::string http_method;
  std::string path;
  if (check_data->FindHeaderByType(CheckData::HEADER_METHOD, &http_method) &&
      check_data->FindHeaderByType(CheckData::HEADER_PATH, &path)) {
    api_spec_parser_->AddAttributes(http_method, path, &request->attributes);
  }

  std::string api_key;
  if (api_spec_parser_->ExtractApiKey(check_data, &api_key)) {
    (*request->attributes
          .mutable_attributes())[utils::AttributeName::kRequestApiKey]
        .set_string_value(api_key);
  }
}

// Add quota requirements from quota configs.
void ServiceContext::AddQuotas(RequestContext *request) const {
  for (const auto &parser : quota_parsers_) {
    parser->GetRequirements(request->attributes, &request->quotas);
  }
}

}  // namespace http
}  // namespace control
}  // namespace istio
