// Copyright 2016 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "contrib/endpoints/src/grpc/transcoding/response_to_json_translator.h"

#include <string>

#include "google/protobuf/io/zero_copy_stream_impl_lite.h"
#include "google/protobuf/stubs/status.h"
#include "google/protobuf/util/json_util.h"
#include "google/protobuf/util/type_resolver.h"

namespace google {
namespace api_manager {

namespace transcoding {

ResponseToJsonTranslator::ResponseToJsonTranslator(
    ::google::protobuf::util::TypeResolver* type_resolver, std::string type_url,
    bool streaming, TranscoderInputStream* in)
    : type_resolver_(type_resolver),
      type_url_(std::move(type_url)),
      streaming_(streaming),
      reader_(in),
      first_(true),
      finished_(false) {}

bool ResponseToJsonTranslator::NextMessage(std::string* message) {
  if (Finished()) {
    // All done
    return false;
  }
  // Try to read a message
  auto proto_in = reader_.NextMessage();
  if (proto_in) {
    std::string json_out;
    if (TranslateMessage(proto_in.get(), &json_out)) {
      *message = std::move(json_out);
      if (!streaming_) {
        // This is a non-streaming call, so we don't expect more messages.
        finished_ = true;
      }
      return true;
    } else {
      // TranslateMessage() failed - return false. The error details are stored
      // in status_.
      return false;
    }
  } else if (streaming_ && reader_.Finished()) {
    // This is a streaming call and the input is finished. Return the final ']'
    // or "[]" in case this was an empty stream.
    *message = first_ ? "[]" : "]";
    finished_ = true;
    return true;
  } else {
    // Don't have an input message
    return false;
  }
}

namespace {

// A helper to write a single char to a ZeroCopyOutputStream
bool WriteChar(::google::protobuf::io::ZeroCopyOutputStream* stream, char c) {
  int size = 0;
  void* data = 0;
  if (!stream->Next(&data, &size) || 0 == size) {
    return false;
  }
  // Write the char to the first byte of the buffer and return the rest size-1
  // bytes to the stream.
  *reinterpret_cast<char*>(data) = c;
  stream->BackUp(size - 1);
  return true;
}

}  // namespace

bool ResponseToJsonTranslator::TranslateMessage(
    ::google::protobuf::io::ZeroCopyInputStream* proto_in,
    std::string* json_out) {
  ::google::protobuf::io::StringOutputStream json_stream(json_out);

  if (streaming_) {
    if (first_) {
      // This is a streaming call and this is the first message, so prepend the
      // output JSON with a '['.
      if (!WriteChar(&json_stream, '[')) {
        status_ = ::google::protobuf::util::Status(
            ::google::protobuf::util::error::INTERNAL,
            "Failed to build the response message.");
        return false;
      }
      first_ = false;
    } else {
      // For streaming calls add a ',' before each message except the first.
      if (!WriteChar(&json_stream, ',')) {
        status_ = ::google::protobuf::util::Status(
            ::google::protobuf::util::error::INTERNAL,
            "Failed to build the response message.");
        return false;
      }
    }
  }

  // Do the actual translation.
  status_ = ::google::protobuf::util::BinaryToJsonStream(
      type_resolver_, type_url_, proto_in, &json_stream);
  if (!status_.ok()) {
    return false;
  }

  return true;
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
