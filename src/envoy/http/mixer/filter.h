/* Copyright 2018 Istio Authors. All Rights Reserved.
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

#include "common/common/logger.h"
#include "envoy/access_log/access_log.h"
#include "envoy/http/filter.h"
#include "src/envoy/http/mixer/control.h"

namespace Envoy {
namespace Http {
namespace Mixer {

class Filter : public Http::StreamDecoderFilter,
               public AccessLog::Instance,
               public Logger::Loggable<Logger::Id::filter> {
 public:
  Filter(Control& control);

  // Implementing virtual functions for StreamDecoderFilter
  FilterHeadersStatus decodeHeaders(HeaderMap& headers, bool) override;
  FilterDataStatus decodeData(Buffer::Instance& data, bool end_stream) override;
  FilterTrailersStatus decodeTrailers(HeaderMap&) override;
  void setDecoderFilterCallbacks(
      StreamDecoderFilterCallbacks& callbacks) override;
  void onDestroy() override;

  // This is the callback function when Check is done.
  void completeCheck(const ::google::protobuf::util::Status& status);

  // Called when the request is completed.
  virtual void log(const HeaderMap* request_headers,
                   const HeaderMap* response_headers,
                   const RequestInfo::RequestInfo& request_info) override;

 private:
  // Read per-route config.
  void ReadPerRouteConfig(
      const Router::RouteEntry* entry,
      ::istio::control::http::Controller::PerRouteConfig* config);

  // The control object.
  Control& control_;
  // The request handler.
  std::unique_ptr<::istio::control::http::RequestHandler> handler_;
  // The pending callback object.
  istio::mixerclient::CancelFunc cancel_check_;

  enum State { NotStarted, Calling, Complete, Responded };
  // The state
  State state_;
  bool initiating_call_;

  // The filter callback.
  StreamDecoderFilterCallbacks* decoder_callbacks_;
};

}  // namespace Mixer
}  // namespace Http
}  // namespace Envoy
