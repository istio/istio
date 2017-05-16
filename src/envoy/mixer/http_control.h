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

#pragma once

#include <memory>

#include "common/common/logger.h"
#include "common/http/headers.h"
#include "envoy/http/access_log.h"
#include "include/client.h"
#include "src/envoy/mixer/config.h"

namespace Envoy {
namespace Http {
namespace Mixer {

// Store data from Check to report
struct HttpRequestData {
  ::istio::mixer_client::Attributes attributes;
};
typedef std::shared_ptr<HttpRequestData> HttpRequestDataPtr;

// The mixer client class to control HTTP requests.
// It has Check() to validate if a request can be processed.
// At the end of request, call Report().
class HttpControl final : public Logger::Loggable<Logger::Id::http> {
 public:
  // The constructor.
  HttpControl(const MixerConfig& mixer_config);

  // Make mixer check call.
  void Check(HttpRequestDataPtr request_data, HeaderMap& headers,
             std::string origin_user, ::istio::mixer_client::DoneFunc on_done);

  void Quota(HttpRequestDataPtr request_data,
             ::istio::mixer_client::DoneFunc on_done);

  // Make mixer report call.
  void Report(HttpRequestDataPtr request_data,
              const HeaderMap* response_headers,
              const AccessLog::RequestInfo& request_info, int check_status_code,
              ::istio::mixer_client::DoneFunc on_done);

 private:
  void FillCheckAttributes(HeaderMap& header_map,
                           ::istio::mixer_client::Attributes* attr);

  // The mixer client
  std::unique_ptr<::istio::mixer_client::MixerClient> mixer_client_;
  // The mixer config
  const MixerConfig& mixer_config_;
  // Quota attributes; extracted from envoy filter config.
  ::istio::mixer_client::Attributes quota_attributes_;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
