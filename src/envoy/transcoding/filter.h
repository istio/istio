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

#include "envoy/buffer/buffer.h"
#include "envoy/http/filter.h"
#include "envoy/http/header_map.h"

#include "src/envoy/transcoding/config.h"
#include "src/envoy/transcoding/envoy_input_stream.h"

namespace Envoy {
namespace Grpc {
namespace Transcoding {

class Instance : public Http::StreamFilter,
                 public Logger::Loggable<Logger::Id::http2> {
 public:
  Instance(Config& config);

  Http::FilterHeadersStatus decodeHeaders(Http::HeaderMap& headers,
                                          bool end_stream) override;

  Http::FilterDataStatus decodeData(Buffer::Instance& data,
                                    bool end_stream) override;

  Http::FilterTrailersStatus decodeTrailers(Http::HeaderMap& trailers) override;

  void setDecoderFilterCallbacks(
      Http::StreamDecoderFilterCallbacks& callbacks) override;

  Http::FilterHeadersStatus encodeHeaders(Http::HeaderMap& headers,
                                          bool end_stream) override;

  Http::FilterDataStatus encodeData(Buffer::Instance& data,
                                    bool end_stream) override;

  Http::FilterTrailersStatus encodeTrailers(Http::HeaderMap& trailers) override;

  void setEncoderFilterCallbacks(
      Http::StreamEncoderFilterCallbacks& callbacks) override;

  void onDestroy() override {}

 private:
  bool ReadToBuffer(google::protobuf::io::ZeroCopyInputStream* stream,
                    Buffer::Instance& data);

  Config& config_;
  std::unique_ptr<google::grpc::transcoding::Transcoder> transcoder_;
  EnvoyInputStream request_in_;
  EnvoyInputStream response_in_;
  Http::StreamDecoderFilterCallbacks* decoder_callbacks_{nullptr};
  Http::StreamEncoderFilterCallbacks* encoder_callbacks_{nullptr};

  bool error_{false};
};

}  // namespace Transcoding
}  // namespace Grpc
}  // namespace Envoy
