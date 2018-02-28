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

#include "src/envoy/http/mixer/filter.h"

#include "common/common/base64.h"
#include "include/istio/utils/status.h"
#include "src/envoy/http/mixer/check_data.h"
#include "src/envoy/http/mixer/header_update.h"
#include "src/envoy/http/mixer/report_data.h"
#include "src/envoy/utils/grpc_transport.h"
#include "src/envoy/utils/utils.h"

using ::google::protobuf::util::Status;
using ::istio::mixer::v1::config::client::ServiceConfig;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// Per route opaque data for "destination.service".
const std::string kPerRouteDestinationService("destination.service");
// Per route opaque data name "mixer" is base64(JSON(ServiceConfig))
const std::string kPerRouteMixer("mixer");
// Per route opaque data name "mixer_sha" is SHA(JSON(ServiceConfig))
const std::string kPerRouteMixerSha("mixer_sha");

// Read a string value from a string map.
bool ReadStringMap(const std::multimap<std::string, std::string>& string_map,
                   const std::string& name, std::string* value) {
  auto it = string_map.find(name);
  if (it != string_map.end()) {
    *value = it->second;
    return true;
  }
  return false;
}

}  // namespace

Filter::Filter(Control& control)
    : control_(control), state_(NotStarted), initiating_call_(false) {
  ENVOY_LOG(debug, "Called Mixer::Filter : {}", __func__);
}

void Filter::ReadPerRouteConfig(
    const Router::RouteEntry* entry,
    ::istio::control::http::Controller::PerRouteConfig* config) {
  if (entry == nullptr) {
    return;
  }

  const auto& string_map = entry->opaqueConfig();
  ReadStringMap(string_map, kPerRouteDestinationService,
                &config->destination_service);

  if (!ReadStringMap(string_map, kPerRouteMixerSha,
                     &config->service_config_id) ||
      config->service_config_id.empty()) {
    return;
  }

  if (control_.controller()->LookupServiceConfig(config->service_config_id)) {
    return;
  }

  std::string config_base64;
  if (!ReadStringMap(string_map, kPerRouteMixer, &config_base64)) {
    ENVOY_LOG(warn, "Service {} missing [mixer] per-route attribute",
              config->destination_service);
    return;
  }
  std::string config_json = Base64::decode(config_base64);
  if (config_json.empty()) {
    ENVOY_LOG(warn, "Service {} invalid base64 config data",
              config->destination_service);
    return;
  }
  ServiceConfig config_pb;
  auto status = Utils::ParseJsonMessage(config_json, &config_pb);
  if (!status.ok()) {
    ENVOY_LOG(warn,
              "Service {} failed to convert JSON config to protobuf, error: {}",
              config->destination_service, status.ToString());
    return;
  }
  control_.controller()->AddServiceConfig(config->service_config_id, config_pb);
  ENVOY_LOG(info, "Service {}, config_id {}, config: {}",
            config->destination_service, config->service_config_id,
            config_pb.DebugString());
}

FilterHeadersStatus Filter::decodeHeaders(HeaderMap& headers, bool) {
  ENVOY_LOG(debug, "Called Mixer::Filter : {}", __func__);

  ::istio::control::http::Controller::PerRouteConfig config;
  auto route = decoder_callbacks_->route();
  if (route) {
    ReadPerRouteConfig(route->routeEntry(), &config);
  }
  handler_ = control_.controller()->CreateRequestHandler(config);

  state_ = Calling;
  initiating_call_ = true;
  CheckData check_data(headers, decoder_callbacks_->connection());
  HeaderUpdate header_update(&headers);
  cancel_check_ = handler_->Check(
      &check_data, &header_update, control_.GetCheckTransport(&headers),
      [this](const Status& status) { completeCheck(status); });
  initiating_call_ = false;

  if (state_ == Complete) {
    return FilterHeadersStatus::Continue;
  }
  ENVOY_LOG(debug, "Called Mixer::Filter : {} Stop", __func__);
  return FilterHeadersStatus::StopIteration;
}

FilterDataStatus Filter::decodeData(Buffer::Instance& data, bool end_stream) {
  ENVOY_LOG(debug, "Called Mixer::Filter : {} ({}, {})", __func__,
            data.length(), end_stream);
  if (state_ == Calling) {
    return FilterDataStatus::StopIterationAndBuffer;
  }
  return FilterDataStatus::Continue;
}

FilterTrailersStatus Filter::decodeTrailers(HeaderMap&) {
  ENVOY_LOG(debug, "Called Mixer::Filter : {}", __func__);
  if (state_ == Calling) {
    return FilterTrailersStatus::StopIteration;
  }
  return FilterTrailersStatus::Continue;
}

void Filter::setDecoderFilterCallbacks(
    StreamDecoderFilterCallbacks& callbacks) {
  ENVOY_LOG(debug, "Called Mixer::Filter : {}", __func__);
  decoder_callbacks_ = &callbacks;
}

void Filter::completeCheck(const Status& status) {
  ENVOY_LOG(debug, "Called Mixer::Filter : check complete {}",
            status.ToString());
  // This stream has been reset, abort the callback.
  if (state_ == Responded) {
    return;
  }
  if (!status.ok() && state_ != Responded) {
    state_ = Responded;
    int status_code = ::istio::utils::StatusHttpCode(status.error_code());
    Utility::sendLocalReply(*decoder_callbacks_, false, Code(status_code),
                            status.ToString());
    return;
  }

  state_ = Complete;
  if (!initiating_call_) {
    decoder_callbacks_->continueDecoding();
  }
}

void Filter::onDestroy() {
  ENVOY_LOG(debug, "Called Mixer::Filter : {} state: {}", __func__, state_);
  if (state_ != Calling) {
    cancel_check_ = nullptr;
  }
  state_ = Responded;
  if (cancel_check_) {
    ENVOY_LOG(debug, "Cancelling check call");
    cancel_check_();
    cancel_check_ = nullptr;
  }
}

void Filter::log(const HeaderMap* request_headers,
                 const HeaderMap* response_headers,
                 const RequestInfo::RequestInfo& request_info) {
  ENVOY_LOG(debug, "Called Mixer::Filter : {}", __func__);
  if (!handler_) {
    if (request_headers == nullptr) {
      return;
    }

    // Here Request is rejected by other filters, Mixer filter is not called.
    ::istio::control::http::Controller::PerRouteConfig config;
    ReadPerRouteConfig(request_info.routeEntry(), &config);
    handler_ = control_.controller()->CreateRequestHandler(config);

    CheckData check_data(*request_headers, nullptr);
    handler_->ExtractRequestAttributes(&check_data);
  }
  ReportData report_data(response_headers, request_info);
  handler_->Report(&report_data);
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
