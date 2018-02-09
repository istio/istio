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

#include "common/common/base64.h"
#include "common/common/logger.h"
#include "common/config/utility.h"
#include "common/http/headers.h"
#include "common/http/utility.h"
#include "control/include/utils/status.h"
#include "envoy/registry/registry.h"
#include "envoy/ssl/connection.h"
#include "envoy/thread_local/thread_local.h"
#include "google/protobuf/util/json_util.h"
#include "server/config/network/http_connection_manager.h"
#include "src/envoy/auth/jwt.h"
#include "src/envoy/auth/jwt_authenticator.h"
#include "src/envoy/mixer/config.h"
#include "src/envoy/mixer/grpc_transport.h"
#include "src/envoy/mixer/mixer_control.h"
#include "src/envoy/mixer/utils.h"

#include <map>
#include <mutex>
#include <thread>

using ::google::protobuf::util::Status;
using HttpCheckData = ::istio::mixer_control::http::CheckData;
using HttpHeaderUpdate = ::istio::mixer_control::http::HeaderUpdate;
using HttpReportData = ::istio::mixer_control::http::ReportData;
using ::istio::mixer::v1::config::client::EndUserAuthenticationPolicySpec;
using ::istio::mixer::v1::config::client::ServiceConfig;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// Switch to turn off mixer both check and report
// They can be overrided by "mixer_check" and "mixer_report" flags.
const std::string kJsonNameMixerControl("mixer_control");

// Switch to turn on/off mixer check only.
const std::string kJsonNameMixerCheck("mixer_check");

// Switch to turn on/off mixer report only.
const std::string kJsonNameMixerReport("mixer_report");

// The prefix in route opaque data to define
// a sub string map of mixer attributes passed to mixer for the route.
const std::string kPrefixMixerAttributes("mixer_attributes.");

// Per route opaque data for "destination.service".
const std::string kPerRouteDestinationService("destination.service");
// Per route opaque data name "mixer" is base64(JSON(ServiceConfig))
const std::string kPerRouteMixer("mixer");
// Per route opaque data name "mixer_sha" is SHA(JSON(ServiceConfig))
const std::string kPerRouteMixerSha("mixer_sha");

// The HTTP header to forward Istio attributes.
const LowerCaseString kIstioAttributeHeader("x-istio-attributes");

// Referer header
const LowerCaseString kRefererHeaderKey("referer");

// Set of headers excluded from request.headers attribute.
const std::set<std::string> RequestHeaderExclusives = {
    kIstioAttributeHeader.get(),
};

// Set of headers excluded from response.headers attribute.
const std::set<std::string> ResponseHeaderExclusives = {};

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

class Config : public Logger::Loggable<Logger::Id::http> {
 private:
  Upstream::ClusterManager& cm_;
  HttpMixerConfig mixer_config_;
  ThreadLocal::SlotPtr tls_;

 public:
  Config(const Json::Object& config,
         Server::Configuration::FactoryContext& context)
      : cm_(context.clusterManager()),
        tls_(context.threadLocal().allocateSlot()) {
    mixer_config_.Load(config);
    Runtime::RandomGenerator& random = context.random();
    tls_->set(
        [this, &random](Event::Dispatcher& dispatcher)
            -> ThreadLocal::ThreadLocalObjectSharedPtr {
              return ThreadLocal::ThreadLocalObjectSharedPtr(
                  new HttpMixerControl(mixer_config_, cm_, dispatcher, random));
            });
  }

  HttpMixerControl& mixer_control() {
    return tls_->getTyped<HttpMixerControl>();
  }

  std::unique_ptr<EndUserAuthenticationPolicySpec> auth_config() {
    auto spec = std::unique_ptr<EndUserAuthenticationPolicySpec>(
        new EndUserAuthenticationPolicySpec);
    for (const auto& it : mixer_config_.http_config.service_configs()) {
      if (it.second.has_end_user_authn_spec()) {
        spec->MergeFrom(it.second.end_user_authn_spec());
      }
    }
    if (spec->jwts_size() == 0) {
      spec.reset();
    }
    return spec;
  }
};

typedef std::shared_ptr<Config> ConfigPtr;

class HeaderUpdate : public HttpHeaderUpdate,
                     public Logger::Loggable<Logger::Id::http> {
  HeaderMap* headers_;

 public:
  HeaderUpdate(HeaderMap* headers) : headers_(headers) {}

  void RemoveIstioAttributes() override {
    headers_->remove(kIstioAttributeHeader);
  }

  // base64 encode data, and add it to the HTTP header.
  void AddIstioAttributes(const std::string& data) override {
    std::string base64 = Base64::encode(data.c_str(), data.size());
    ENVOY_LOG(debug, "Mixer forward attributes set: {}", base64);
    headers_->addReferenceKey(kIstioAttributeHeader, base64);
  }
};

class CheckData : public HttpCheckData,
                  public Logger::Loggable<Logger::Id::http> {
  const HeaderMap& headers_;
  const Network::Connection* connection_;
  Utility::QueryParams query_params_;

 public:
  CheckData(const HeaderMap& headers, const Network::Connection* connection)
      : headers_(headers), connection_(connection) {
    if (headers_.Path()) {
      query_params_ = Utility::parseQueryString(std::string(
          headers_.Path()->value().c_str(), headers_.Path()->value().size()));
    }
  }

  // Find "x-istio-attributes" headers, if found base64 decode
  // its value and remove it from the headers.
  bool ExtractIstioAttributes(std::string* data) const override {
    // Extract attributes from x-istio-attributes header
    const HeaderEntry* entry = headers_.get(kIstioAttributeHeader);
    if (entry) {
      *data = Base64::decode(
          std::string(entry->value().c_str(), entry->value().size()));
      return true;
    }
    return false;
  }

  bool GetSourceIpPort(std::string* ip, int* port) const override {
    if (connection_) {
      return Utils::GetIpPort(connection_->remoteAddress()->ip(), ip, port);
    }
    return false;
  }

  bool GetSourceUser(std::string* user) const override {
    return Utils::GetSourceUser(connection_, user);
  }

  std::map<std::string, std::string> GetRequestHeaders() const override {
    return Utils::ExtractHeaders(headers_, RequestHeaderExclusives);
  }

  bool IsMutualTLS() const override { return Utils::IsMutualTLS(connection_); }

  bool FindHeaderByType(HttpCheckData::HeaderType header_type,
                        std::string* value) const override {
    switch (header_type) {
      case HttpCheckData::HEADER_PATH:
        if (headers_.Path()) {
          *value = std::string(headers_.Path()->value().c_str(),
                               headers_.Path()->value().size());
          return true;
        }
        break;
      case HttpCheckData::HEADER_HOST:
        if (headers_.Host()) {
          *value = std::string(headers_.Host()->value().c_str(),
                               headers_.Host()->value().size());
          return true;
        }
        break;
      case HttpCheckData::HEADER_SCHEME:
        if (headers_.Scheme()) {
          *value = std::string(headers_.Scheme()->value().c_str(),
                               headers_.Scheme()->value().size());
          return true;
        }
        break;
      case HttpCheckData::HEADER_USER_AGENT:
        if (headers_.UserAgent()) {
          *value = std::string(headers_.UserAgent()->value().c_str(),
                               headers_.UserAgent()->value().size());
          return true;
        }
        break;
      case HttpCheckData::HEADER_METHOD:
        if (headers_.Method()) {
          *value = std::string(headers_.Method()->value().c_str(),
                               headers_.Method()->value().size());
          return true;
        }
        break;
      case HttpCheckData::HEADER_REFERER: {
        const HeaderEntry* referer = headers_.get(kRefererHeaderKey);
        if (referer) {
          *value =
              std::string(referer->value().c_str(), referer->value().size());
          return true;
        }
      } break;
    }
    return false;
  }

  bool FindHeaderByName(const std::string& name,
                        std::string* value) const override {
    const HeaderEntry* entry = headers_.get(LowerCaseString(name));
    if (entry) {
      *value = std::string(entry->value().c_str(), entry->value().size());
      return true;
    }
    return false;
  }

  bool FindQueryParameter(const std::string& name,
                          std::string* value) const override {
    const auto& it = query_params_.find(name);
    if (it != query_params_.end()) {
      *value = it->second;
      return true;
    }
    return false;
  }

  bool FindCookie(const std::string& name, std::string* value) const override {
    std::string cookie = Utility::parseCookieValue(headers_, name);
    if (cookie != "") {
      *value = cookie;
      return true;
    }
    return false;
  }

  bool GetJWTPayload(
      std::map<std::string, std::string>* payload) const override {
    const HeaderEntry* entry =
        headers_.get(Auth::JwtAuthenticator::JwtPayloadKey());
    if (!entry) {
      return false;
    }
    std::string value(entry->value().c_str(), entry->value().size());
    std::string payload_str = Auth::Base64UrlDecode(value);
    // Return an empty string if Base64 decode fails.
    if (payload_str.empty()) {
      ENVOY_LOG(error, "Invalid {} header, invalid base64: {}",
                Auth::JwtAuthenticator::JwtPayloadKey().get(), value);
      return false;
    }
    try {
      auto json_obj = Json::Factory::loadFromString(payload_str);
      json_obj->iterate(
          [payload](const std::string& key, const Json::Object& obj) -> bool {
            // will throw execption if value type is not string.
            try {
              (*payload)[key] = obj.asString();
            } catch (...) {
            }
            return true;
          });
    } catch (...) {
      ENVOY_LOG(error, "Invalid {} header, invalid json: {}",
                Auth::JwtAuthenticator::JwtPayloadKey().get(), payload_str);
      return false;
    }
    return true;
  }
};

class ReportData : public HttpReportData {
  const HeaderMap* headers_;
  const RequestInfo::RequestInfo& info_;

 public:
  ReportData(const HeaderMap* headers, const RequestInfo::RequestInfo& info)
      : headers_(headers), info_(info) {}

  std::map<std::string, std::string> GetResponseHeaders() const override {
    if (headers_) {
      return Utils::ExtractHeaders(*headers_, ResponseHeaderExclusives);
    }
    return std::map<std::string, std::string>();
  }

  void GetReportInfo(HttpReportData::ReportInfo* data) const override {
    data->received_bytes = info_.bytesReceived();
    data->send_bytes = info_.bytesSent();
    data->duration =
        std::chrono::duration_cast<std::chrono::nanoseconds>(info_.duration());
    if (info_.responseCode().valid()) {
      data->response_code = info_.responseCode().value();
    }
  }
};

class Instance : public Http::StreamDecoderFilter,
                 public AccessLog::Instance,
                 public Logger::Loggable<Logger::Id::http> {
 private:
  HttpMixerControl& mixer_control_;
  std::unique_ptr<::istio::mixer_control::http::RequestHandler> handler_;
  istio::mixer_client::CancelFunc cancel_check_;

  enum State { NotStarted, Calling, Complete, Responded };
  State state_;

  StreamDecoderFilterCallbacks* decoder_callbacks_;

  bool initiating_call_;

  // check mixer on/off flags in route opaque data
  void check_mixer_route_flags(bool* check_disabled, bool* report_disabled) {
    // Both check and report are disabled by default.
    *check_disabled = true;
    *report_disabled = true;
    auto route = decoder_callbacks_->route();
    if (route != nullptr) {
      auto entry = route->routeEntry();
      if (entry != nullptr) {
        auto control_key = entry->opaqueConfig().find(kJsonNameMixerControl);
        if (control_key != entry->opaqueConfig().end() &&
            control_key->second == "on") {
          *check_disabled = false;
          *report_disabled = false;
        }
        auto check_key = entry->opaqueConfig().find(kJsonNameMixerCheck);
        if (check_key != entry->opaqueConfig().end() &&
            check_key->second == "on") {
          *check_disabled = false;
        }
        auto report_key = entry->opaqueConfig().find(kJsonNameMixerReport);
        if (report_key != entry->opaqueConfig().end() &&
            report_key->second == "on") {
          *report_disabled = false;
        }
      }
    }
  }

  // Extract a prefixed string map from route opaque config.
  // Route opaque config only supports flat name value pair, have to use
  // prefix to create a sub string map. such as:
  //    prefix.key1 = value1
  std::map<std::string, std::string> GetRouteStringMap(
      const std::string& prefix) {
    std::map<std::string, std::string> attrs;
    auto route = decoder_callbacks_->route();
    if (route != nullptr) {
      auto entry = route->routeEntry();
      if (entry != nullptr) {
        for (const auto& it : entry->opaqueConfig()) {
          if (it.first.substr(0, prefix.size()) == prefix) {
            attrs[it.first.substr(prefix.size(), std::string::npos)] =
                it.second;
          }
        }
      }
    }
    return attrs;
  }

  void ReadPerRouteConfig(
      ::istio::mixer_control::http::Controller::PerRouteConfig* config) {
    auto route = decoder_callbacks_->route();
    if (route == nullptr) {
      return;
    }
    auto entry = route->routeEntry();
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

    if (mixer_control_.controller()->LookupServiceConfig(
            config->service_config_id)) {
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
    auto status =
        ::google::protobuf::util::JsonStringToMessage(config_json, &config_pb);
    if (!status.ok()) {
      ENVOY_LOG(
          warn,
          "Service {} failed to convert JSON config to protobuf, error: {}",
          config->destination_service, status.ToString());
      return;
    }
    mixer_control_.controller()->AddServiceConfig(config->service_config_id,
                                                  config_pb);
    ENVOY_LOG(info, "Service {}, config_id {}, config: {}",
              config->destination_service, config->service_config_id,
              config_pb.DebugString());
  }

 public:
  Instance(ConfigPtr config)
      : mixer_control_(config->mixer_control()),
        state_(NotStarted),
        initiating_call_(false) {
    ENVOY_LOG(debug, "Called Mixer::Instance : {}", __func__);
  }

  FilterHeadersStatus decodeHeaders(HeaderMap& headers, bool) override {
    ENVOY_LOG(debug, "Called Mixer::Instance : {}", __func__);

    ::istio::mixer_control::http::Controller::PerRouteConfig config;
    ServiceConfig legacy_config;
    if (mixer_control_.has_v2_config()) {
      ReadPerRouteConfig(&config);
    } else {
      bool check_disabled, report_disabled;
      check_mixer_route_flags(&check_disabled, &report_disabled);

      HttpMixerConfig::CreateLegacyRouteConfig(
          check_disabled, report_disabled,
          GetRouteStringMap(kPrefixMixerAttributes), &legacy_config);
      config.legacy_config = &legacy_config;
    }
    handler_ = mixer_control_.controller()->CreateRequestHandler(config);

    state_ = Calling;
    initiating_call_ = true;
    CheckData check_data(headers, decoder_callbacks_->connection());
    HeaderUpdate header_update(&headers);
    cancel_check_ = handler_->Check(
        &check_data, &header_update, mixer_control_.GetCheckTransport(&headers),
        [this](const Status& status) { completeCheck(status); });
    initiating_call_ = false;

    if (state_ == Complete) {
      return FilterHeadersStatus::Continue;
    }
    ENVOY_LOG(debug, "Called Mixer::Instance : {} Stop", __func__);
    return FilterHeadersStatus::StopIteration;
  }

  FilterDataStatus decodeData(Buffer::Instance& data,
                              bool end_stream) override {
    ENVOY_LOG(debug, "Called Mixer::Instance : {} ({}, {})", __func__,
              data.length(), end_stream);
    if (state_ == Calling) {
      return FilterDataStatus::StopIterationAndBuffer;
    }
    return FilterDataStatus::Continue;
  }

  FilterTrailersStatus decodeTrailers(HeaderMap&) override {
    ENVOY_LOG(debug, "Called Mixer::Instance : {}", __func__);
    if (state_ == Calling) {
      return FilterTrailersStatus::StopIteration;
    }
    return FilterTrailersStatus::Continue;
  }

  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override {
    ENVOY_LOG(debug, "Called Mixer::Instance : {}", __func__);
    decoder_callbacks_ = &callbacks;
  }

  void completeCheck(const Status& status) {
    ENVOY_LOG(debug, "Called Mixer::Instance : check complete {}",
              status.ToString());
    // This stream has been reset, abort the callback.
    if (state_ == Responded) {
      return;
    }
    if (!status.ok() && state_ != Responded) {
      state_ = Responded;
      int status_code =
          ::istio::mixer_control::utils::StatusHttpCode(status.error_code());
      Utility::sendLocalReply(*decoder_callbacks_, false, Code(status_code),
                              status.ToString());
      return;
    }

    state_ = Complete;
    if (!initiating_call_) {
      decoder_callbacks_->continueDecoding();
    }
  }

  void onDestroy() override {
    ENVOY_LOG(debug, "Called Mixer::Instance : {} state: {}", __func__, state_);
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

  virtual void log(const HeaderMap* request_headers,
                   const HeaderMap* response_headers,
                   const RequestInfo::RequestInfo& request_info) override {
    ENVOY_LOG(debug, "Called Mixer::Instance : {}", __func__);
    if (!handler_) {
      if (request_headers == nullptr) {
        return;
      }

      // Here Request is rejected by other filters, Mixer filter is not called.
      // TODO: get downstream connection related attributes such as source.ip
      // But at this stage, not sure if callback_->connection is safe to call.
      // Similarly, it is better to get per-route attributes, but not sure if
      // it is safe to call callback_->route().
      ::istio::mixer_control::http::Controller::PerRouteConfig config;
      handler_ = mixer_control_.controller()->CreateRequestHandler(config);

      CheckData check_data(*request_headers, nullptr);
      handler_->ExtractRequestAttributes(&check_data);
    }
    ReportData report_data(response_headers, request_info);
    handler_->Report(&report_data);
  }
};

}  // namespace Mixer
}  // namespace Http

namespace Server {
namespace Configuration {

class MixerConfigFactory : public NamedHttpFilterConfigFactory {
 public:
  HttpFilterFactoryCb createFilterFactory(const Json::Object& config,
                                          const std::string&,
                                          FactoryContext& context) override {
    Http::Mixer::ConfigPtr mixer_config(
        new Http::Mixer::Config(config, context));

    HttpFilterFactoryCb auth_filter_cb;
    auto auth_config = mixer_config->auth_config();
    if (auth_config) {
      auto& auth_factory =
          Config::Utility::getAndCheckFactory<NamedHttpFilterConfigFactory>(
              std::string("jwt-auth"));
      auto proto_config = auth_factory.createEmptyConfigProto();
      MessageUtil::jsonConvert(*auth_config, *proto_config);
      auth_filter_cb =
          auth_factory.createFilterFactoryFromProto(*proto_config, "", context);
    }
    return [mixer_config, auth_filter_cb](
               Http::FilterChainFactoryCallbacks& callbacks) -> void {
      if (auth_filter_cb) {
        auth_filter_cb(callbacks);
      }
      std::shared_ptr<Http::Mixer::Instance> instance =
          std::make_shared<Http::Mixer::Instance>(mixer_config);
      callbacks.addStreamDecoderFilter(
          Http::StreamDecoderFilterSharedPtr(instance));
      callbacks.addAccessLogHandler(AccessLog::InstanceSharedPtr(instance));
    };
  }
  std::string name() override { return "mixer"; }
};

static Registry::RegisterFactory<MixerConfigFactory,
                                 NamedHttpFilterConfigFactory>
    register_;

}  // namespace Configuration
}  // namespace Server
}  // namespace Envoy
