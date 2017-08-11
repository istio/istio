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

#include "src/envoy/mixer/mixer_control.h"

#include "common/common/base64.h"
#include "common/common/utility.h"
#include "common/http/utility.h"

#include "src/envoy/mixer/grpc_transport.h"
#include "src/envoy/mixer/string_map.pb.h"
#include "src/envoy/mixer/thread_dispatcher.h"

using ::google::protobuf::util::Status;
using StatusCode = ::google::protobuf::util::error::Code;
using ::istio::mixer_client::Attributes;
using ::istio::mixer_client::CheckOptions;
using ::istio::mixer_client::DoneFunc;
using ::istio::mixer_client::MixerClientOptions;
using ::istio::mixer_client::ReportOptions;
using ::istio::mixer_client::QuotaOptions;

namespace Envoy {
namespace Http {
namespace Mixer {
namespace {

// Define attribute names
const std::string kSourceUser = "source.user";

const std::string kRequestHeaders = "request.headers";
const std::string kRequestHost = "request.host";
const std::string kRequestMethod = "request.method";
const std::string kRequestPath = "request.path";
const std::string kRequestReferer = "request.referer";
const std::string kRequestScheme = "request.scheme";
const std::string kRequestSize = "request.size";
const std::string kRequestTime = "request.time";
const std::string kRequestUserAgent = "request.useragent";

const std::string kResponseCode = "response.code";
const std::string kResponseDuration = "response.duration";
const std::string kResponseHeaders = "response.headers";
const std::string kResponseSize = "response.size";
const std::string kResponseTime = "response.time";

// TCP attributes
// Downstream tcp connection: source ip/port.
const std::string kSourceIp = "source.ip";
const std::string kSourcePort = "source.port";
// Upstream tcp connection: destionation ip/port.
const std::string kTargetIp = "target.ip";
const std::string kTargetPort = "target.port";
const std::string kConnectionReceviedBytes = "connection.received.bytes";
const std::string kConnectionReceviedTotalBytes =
    "connection.received.bytes_total";
const std::string kConnectionSendBytes = "connection.sent.bytes";
const std::string kConnectionSendTotalBytes = "connection.sent.bytes_total";
const std::string kConnectionDuration = "connection.duration";

// Context attributes
const std::string kContextProtocol = "context.protocol";
const std::string kContextTime = "context.time";

// Check status code.
const std::string kCheckStatusCode = "check.status";

// Keys to well-known headers
const LowerCaseString kRefererHeaderKey("referer");

// Check cache size: 10000 cache entries.
const int kCheckCacheEntries = 10000;

CheckOptions GetCheckOptions(const MixerConfig& config) {
  CheckOptions options(kCheckCacheEntries);
  options.cache_keys = config.check_cache_keys;

  if (config.network_fail_policy == "close") {
    options.network_fail_open = false;
  }

  return options;
}

void SetStringAttribute(const std::string& name, const std::string& value,
                        Attributes* attr) {
  if (!value.empty()) {
    attr->attributes[name] = Attributes::StringValue(value);
  }
}

void SetInt64Attribute(const std::string& name, uint64_t value,
                       Attributes* attr) {
  attr->attributes[name] = Attributes::Int64Value(value);
}

std::map<std::string, std::string> ExtractHeaders(const HeaderMap& header_map) {
  std::map<std::string, std::string> headers;
  header_map.iterate(
      [](const HeaderEntry& header, void* context) {
        std::map<std::string, std::string>* header_map =
            static_cast<std::map<std::string, std::string>*>(context);
        (*header_map)[header.key().c_str()] = header.value().c_str();
      },
      &headers);
  return headers;
}

void FillRequestHeaderAttributes(const HeaderMap& header_map,
                                 Attributes* attr) {
  SetStringAttribute(kRequestPath, header_map.Path()->value().c_str(), attr);
  SetStringAttribute(kRequestHost, header_map.Host()->value().c_str(), attr);

  // Since we're in an HTTP filter, if the scheme header doesn't exist we can
  // fill it in with a reasonable value.
  SetStringAttribute(
      kRequestScheme,
      header_map.Scheme() ? header_map.Scheme()->value().c_str() : "http",
      attr);

  if (header_map.UserAgent()) {
    SetStringAttribute(kRequestUserAgent,
                       header_map.UserAgent()->value().c_str(), attr);
  }
  if (header_map.Method()) {
    SetStringAttribute(kRequestMethod, header_map.Method()->value().c_str(),
                       attr);
  }

  const HeaderEntry* referer = header_map.get(kRefererHeaderKey);
  if (referer) {
    std::string val(referer->value().c_str(), referer->value().size());
    SetStringAttribute(kRequestReferer, val, attr);
  }

  attr->attributes[kRequestHeaders] =
      Attributes::StringMapValue(ExtractHeaders(header_map));
}

void FillResponseHeaderAttributes(const HeaderMap* header_map,
                                  Attributes* attr) {
  if (header_map) {
    attr->attributes[kResponseHeaders] =
        Attributes::StringMapValue(ExtractHeaders(*header_map));
  }
}

void FillRequestInfoAttributes(const AccessLog::RequestInfo& info,
                               int check_status_code, Attributes* attr) {
  SetInt64Attribute(kRequestSize, info.bytesReceived(), attr);
  SetInt64Attribute(kResponseSize, info.bytesSent(), attr);

  attr->attributes[kResponseDuration] = Attributes::DurationValue(
      std::chrono::duration_cast<std::chrono::nanoseconds>(info.duration()));

  if (info.responseCode().valid()) {
    SetInt64Attribute(kResponseCode, info.responseCode().value(), attr);
  } else {
    SetInt64Attribute(kResponseCode, check_status_code, attr);
  }
}

void FillCheckAttributes(HeaderMap& header_map, Attributes* attr) {
  // Extract attributes from x-istio-attributes header
  const HeaderEntry* entry = header_map.get(Utils::kIstioAttributeHeader);
  if (entry) {
    ::istio::proxy::mixer::StringMap pb;
    std::string str(entry->value().c_str(), entry->value().size());
    pb.ParseFromString(Base64::decode(str));
    for (const auto& it : pb.map()) {
      SetStringAttribute(it.first, it.second, attr);
    }
    header_map.remove(Utils::kIstioAttributeHeader);
  }

  FillRequestHeaderAttributes(header_map, attr);
}

// A class to wrap envoy timer for mixer client timer.
class EnvoyTimer : public ::istio::mixer_client::Timer {
 public:
  EnvoyTimer(Event::TimerPtr timer) : timer_(std::move(timer)) {}

  void Stop() override { timer_->disableTimer(); }
  void Start(int interval_ms) override {
    timer_->enableTimer(std::chrono::milliseconds(interval_ms));
  }

 private:
  Event::TimerPtr timer_;
};

}  // namespace

MixerControl::MixerControl(const MixerConfig& mixer_config,
                           Upstream::ClusterManager& cm)
    : cm_(cm), mixer_config_(mixer_config) {
  if (GrpcTransport::IsMixerServerConfigured(cm)) {
    MixerClientOptions options(GetCheckOptions(mixer_config), ReportOptions(),
                               QuotaOptions());
    options.check_transport = CheckGrpcTransport::GetFunc(cm, nullptr);
    options.report_transport = ReportGrpcTransport::GetFunc(cm);

    options.timer_create_func = [](std::function<void()> timer_cb)
        -> std::unique_ptr<::istio::mixer_client::Timer> {
          return std::unique_ptr<::istio::mixer_client::Timer>(
              new EnvoyTimer(GetThreadDispatcher().createTimer(timer_cb)));
        };

    mixer_client_ = ::istio::mixer_client::CreateMixerClient(options);
  } else {
    log().error("Mixer server cluster is not configured");
  }

  mixer_config_.ExtractQuotaAttributes(&quota_attributes_);
}

void MixerControl::SendCheck(HttpRequestDataPtr request_data,
                             const HeaderMap* headers, DoneFunc on_done) {
  for (const auto& attribute : quota_attributes_.attributes) {
    request_data->attributes.attributes[attribute.first] = attribute.second;
  }
  for (const auto& attribute : mixer_config_.mixer_attributes) {
    SetStringAttribute(attribute.first, attribute.second,
                       &request_data->attributes);
  }

  log().debug("Send Check: {}", request_data->attributes.DebugString());
  mixer_client_->Check(request_data->attributes,
                       CheckGrpcTransport::GetFunc(cm_, headers), on_done);
}

void MixerControl::SendReport(HttpRequestDataPtr request_data) {
  log().debug("Send Report: {}", request_data->attributes.DebugString());
  mixer_client_->Report(request_data->attributes);
}

void MixerControl::ForwardAttributes(HeaderMap& headers,
                                     const Utils::StringMap& route_attributes) {
  if (mixer_config_.forward_attributes.empty() && route_attributes.empty()) {
    return;
  }
  std::string serialized_str = Utils::SerializeTwoStringMaps(
      mixer_config_.forward_attributes, route_attributes);
  std::string base64 =
      Base64::encode(serialized_str.c_str(), serialized_str.size());
  log().debug("Mixer forward attributes set: {}", base64);
  headers.addReferenceKey(Utils::kIstioAttributeHeader, base64);
}

void MixerControl::CheckHttp(HttpRequestDataPtr request_data,
                             HeaderMap& headers, std::string source_user,
                             const Utils::StringMap& route_attributes,
                             DoneFunc on_done) {
  if (!mixer_client_) {
    on_done(
        Status(StatusCode::INVALID_ARGUMENT, "Missing mixer_server cluster"));
    return;
  }
  FillCheckAttributes(headers, &request_data->attributes);
  for (const auto& attribute : route_attributes) {
    SetStringAttribute(attribute.first, attribute.second,
                       &request_data->attributes);
  }

  SetStringAttribute(kSourceUser, source_user, &request_data->attributes);

  request_data->attributes.attributes[kRequestTime] =
      Attributes::TimeValue(std::chrono::system_clock::now());
  SetStringAttribute(kContextProtocol, "http", &request_data->attributes);

  SendCheck(request_data, &headers, on_done);
}

void MixerControl::ReportHttp(HttpRequestDataPtr request_data,
                              const HeaderMap* response_headers,
                              const AccessLog::RequestInfo& request_info,
                              int check_status) {
  if (!mixer_client_) {
    return;
  }

  // Use all Check attributes for Report.
  // Add additional Report attributes.
  FillResponseHeaderAttributes(response_headers, &request_data->attributes);

  FillRequestInfoAttributes(request_info, check_status,
                            &request_data->attributes);

  request_data->attributes.attributes[kResponseTime] =
      Attributes::TimeValue(std::chrono::system_clock::now());
  SendReport(request_data);
}

void MixerControl::CheckTcp(HttpRequestDataPtr request_data,
                            Network::Connection& connection,
                            std::string source_user, DoneFunc on_done) {
  if (!mixer_client_) {
    on_done(
        Status(StatusCode::INVALID_ARGUMENT, "Missing mixer_server cluster"));
    return;
  }

  SetStringAttribute(kSourceUser, source_user, &request_data->attributes);

  const Network::Address::Ip* remote_ip = connection.remoteAddress().ip();
  if (remote_ip) {
    SetStringAttribute(kSourceIp, remote_ip->addressAsString(),
                       &request_data->attributes);
    SetInt64Attribute(kSourcePort, remote_ip->port(),
                      &request_data->attributes);
  }

  request_data->attributes.attributes[kContextTime] =
      Attributes::TimeValue(std::chrono::system_clock::now());
  SetStringAttribute(kContextProtocol, "tcp", &request_data->attributes);
  SendCheck(request_data, nullptr, on_done);
}

void MixerControl::ReportTcp(
    HttpRequestDataPtr request_data, uint64_t received_bytes,
    uint64_t send_bytes, int check_status_code,
    std::chrono::nanoseconds duration,
    Upstream::HostDescriptionConstSharedPtr upstreamHost) {
  if (!mixer_client_) {
    return;
  }

  SetInt64Attribute(kConnectionReceviedBytes, received_bytes,
                    &request_data->attributes);
  SetInt64Attribute(kConnectionReceviedTotalBytes, received_bytes,
                    &request_data->attributes);
  SetInt64Attribute(kConnectionSendBytes, send_bytes,
                    &request_data->attributes);
  SetInt64Attribute(kConnectionSendTotalBytes, send_bytes,
                    &request_data->attributes);
  request_data->attributes.attributes[kConnectionDuration] =
      Attributes::DurationValue(duration);

  SetInt64Attribute(kCheckStatusCode, check_status_code,
                    &request_data->attributes);

  if (upstreamHost && upstreamHost->address()) {
    const Network::Address::Ip* target_ip = upstreamHost->address()->ip();
    if (target_ip) {
      SetStringAttribute(kTargetIp, target_ip->addressAsString(),
                         &request_data->attributes);
      SetInt64Attribute(kTargetPort, target_ip->port(),
                        &request_data->attributes);
    }
  }

  request_data->attributes.attributes[kContextTime] =
      Attributes::TimeValue(std::chrono::system_clock::now());
  SendReport(request_data);
}

std::shared_ptr<MixerControl> MixerControlPerThreadStore::Get() {
  std::thread::id id = std::this_thread::get_id();
  std::lock_guard<std::mutex> lock(map_mutex_);
  auto it = instance_map_.find(id);
  if (it != instance_map_.end()) {
    return it->second;
  }
  auto mixer_control = create_func_();
  instance_map_[id] = mixer_control;
  return mixer_control;
}

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
