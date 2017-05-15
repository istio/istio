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
#include "src/envoy/transcoding/filter.h"

#include "google/protobuf/descriptor.h"
#include "google/protobuf/descriptor.pb.h"
#include "google/protobuf/message.h"
#include "google/protobuf/util/type_resolver.h"
#include "google/protobuf/util/type_resolver_util.h"
#include "server/config/network/http_connection_manager.h"

using google::protobuf::FileDescriptor;
using google::protobuf::FileDescriptorSet;
using google::protobuf::DescriptorPool;

namespace Grpc {
namespace Transcoding {

const std::string kTypeUrlPrefix{"type.googleapis.com"};

const std::string kGrpcContentType{"application/grpc"};
const std::string kJsonContentType{"application/json"};
const Http::LowerCaseString kTeHeader{"te"};
const std::string kTeTrailers{"trailers"};

Instance::Instance(Config& config) : config_(config) {}

Http::FilterHeadersStatus Instance::decodeHeaders(Http::HeaderMap& headers,
                                                  bool end_stream) {
  log().debug("Transcoding::Instance::decodeHeaders");

  const google::protobuf::MethodDescriptor* method;
  auto status = config_.CreateTranscoder(headers, &request_in_, &response_in_,
                                         transcoder_, method);
  if (status.ok()) {
    headers.removeContentLength();
    headers.insertContentType().value(kGrpcContentType);
    headers.insertPath().value("/" + method->service()->full_name() + "/" +
                               method->name());
    headers.insertMethod().value(std::string("POST"));

    headers.addStatic(kTeHeader, kTeTrailers);

    if (end_stream) {
      log().debug("header only request");

      request_in_.Finish();

      Buffer::OwnedImpl data;
      ReadToBuffer(transcoder_->RequestOutput(), data);

      if (data.length()) {
        decoder_callbacks_->addDecodedData(data);
      }
    }
  } else {
    log().debug("No transcoding" + status.ToString());
  }

  return Http::FilterHeadersStatus::Continue;
}

Http::FilterDataStatus Instance::decodeData(Buffer::Instance& data,
                                            bool end_stream) {
  log().debug("Transcoding::Instance::decodeData");
  if (transcoder_) {
    request_in_.Move(data);

    ReadToBuffer(transcoder_->RequestOutput(), data);

    // TODO: Check RequesStatus
  }

  return Http::FilterDataStatus::Continue;
}

Http::FilterTrailersStatus Instance::decodeTrailers(Http::HeaderMap& trailers) {
  log().debug("Transcoding::Instance::decodeTrailers");
  if (transcoder_) {
    request_in_.Finish();

    Buffer::OwnedImpl data;
    ReadToBuffer(transcoder_->RequestOutput(), data);

    if (data.length()) {
      decoder_callbacks_->addDecodedData(data);
    }
  }

  return Http::FilterTrailersStatus::Continue;
}

void Instance::setDecoderFilterCallbacks(
    Http::StreamDecoderFilterCallbacks& callbacks) {
  decoder_callbacks_ = &callbacks;
}

Http::FilterHeadersStatus Instance::encodeHeaders(Http::HeaderMap& headers,
                                                  bool end_stream) {
  log().debug("Transcoding::Instance::encodeHeaders");
  if (transcoder_) {
    headers.insertContentType().value(kJsonContentType);
  }
  return Http::FilterHeadersStatus::Continue;
}

Http::FilterDataStatus Instance::encodeData(Buffer::Instance& data,
                                            bool end_stream) {
  log().debug("Transcoding::Instance::encodeData");
  if (transcoder_) {
    response_in_.Move(data);

    if (end_stream) {
      response_in_.Finish();
    }

    ReadToBuffer(transcoder_->ResponseOutput(), data);

    // TODO: Check ResponseStatus
  }

  return Http::FilterDataStatus::Continue;
}

Http::FilterTrailersStatus Instance::encodeTrailers(Http::HeaderMap& trailers) {
  log().debug("Transcoding::Instance::encodeTrailers");
  if (transcoder_) {
    response_in_.Finish();

    Buffer::OwnedImpl data;
    ReadToBuffer(transcoder_->ResponseOutput(), data);

    if (data.length()) {
      encoder_callbacks_->addEncodedData(data);
    }
  }
  return Http::FilterTrailersStatus::Continue;
}

void Instance::setEncoderFilterCallbacks(
    Http::StreamEncoderFilterCallbacks& callbacks) {
  encoder_callbacks_ = &callbacks;
}

bool Instance::ReadToBuffer(google::protobuf::io::ZeroCopyInputStream* stream,
                            Buffer::Instance& data) {
  const void* out;
  int size;
  while (stream->Next(&out, &size)) {
    data.add(out, size);

    if (size == 0) {
      return true;
    }
  }
  return false;
}

}  // namespace Transcoding
}  // namespace Grpc

namespace Server {
namespace Configuration {

class TranscodingConfig : public HttpFilterConfigFactory {
 public:
  HttpFilterFactoryCb tryCreateFilterFactory(
      HttpFilterType type, const std::string& name, const Json::Object& config,
      const std::string&, Server::Instance& server) override {
    if (type != HttpFilterType::Both || name != "transcoding") {
      return nullptr;
    }

    Grpc::Transcoding::ConfigSharedPtr transcoding_config{
        new Grpc::Transcoding::Config(config)};
    return [transcoding_config](
               Http::FilterChainFactoryCallbacks& callbacks) -> void {
      std::shared_ptr<Grpc::Transcoding::Instance> instance =
          std::make_shared<Grpc::Transcoding::Instance>(*transcoding_config);
      callbacks.addStreamFilter(Http::StreamFilterSharedPtr(instance));
    };
  }
};

static RegisterHttpFilterConfigFactory<TranscodingConfig> register_;

}  // namespace Configuration
}  // namespace Server
