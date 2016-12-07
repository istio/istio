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
#include "contrib/endpoints/src/grpc/transcoding/request_stream_translator.h"

#include <memory>
#include <string>

#include "contrib/endpoints/src/grpc/transcoding/request_message_translator.h"
#include "google/protobuf/stubs/stringpiece.h"

namespace google {
namespace api_manager {

namespace transcoding {

namespace pb = google::protobuf;
namespace pbutil = google::protobuf::util;
namespace pberr = google::protobuf::util::error;
namespace pbconv = google::protobuf::util::converter;

RequestStreamTranslator::RequestStreamTranslator(
    google::protobuf::util::TypeResolver& type_resolver, bool output_delimiters,
    RequestInfo request_info)
    : type_resolver_(type_resolver),
      status_(),
      request_info_(std::move(request_info)),
      output_delimiters_(output_delimiters),
      translator_(),
      messages_(),
      depth_(0),
      done_(false) {}

RequestStreamTranslator::~RequestStreamTranslator() {}

bool RequestStreamTranslator::NextMessage(std::string* message) {
  if (!messages_.empty()) {
    *message = std::move(messages_.front());
    messages_.pop_front();
    return true;
  } else {
    return false;
  }
}

bool RequestStreamTranslator::Finished() const {
  return (messages_.empty() && done_) || !status_.ok();
}

RequestStreamTranslator* RequestStreamTranslator::StartObject(
    pb::StringPiece name) {
  if (!status_.ok()) {
    // In error state - return right away
    return this;
  }
  if (depth_ == 0) {
    // In depth_ == 0 case we expect only StartList()
    status_ = pbutil::Status(pberr::INVALID_ARGUMENT,
                             "Expected an array instead of an object");
    return this;
  }
  if (depth_ == 1) {
    // An element of the outermost array - start the ProtoMessageTranslator to
    // to translate the array.
    StartMessageTranslator();
  }
  translator_->Input().StartObject(name);
  ++depth_;
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::EndObject() {
  if (!status_.ok()) {
    // In error state - return right away
    return this;
  }
  --depth_;
  if (depth_ < 1) {
    status_ =
        pbutil::Status(pberr::INVALID_ARGUMENT, "Mismatched end of object.");
    return this;
  }
  translator_->Input().EndObject();
  if (depth_ == 1) {
    // An element of the outermost array was closed - end the
    // ProtoMessageTranslator to save the translated message.
    EndMessageTranslator();
  }
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::StartList(
    pb::StringPiece name) {
  if (!status_.ok()) {
    // In error state - return right away
    return this;
  }
  if (depth_ == 0) {
    // Started the outermost array - do nothing
    ++depth_;
    return this;
  }
  if (depth_ == 1) {
    // This means we have an array of arrays. This can happen if the HTTP body
    // is mapped to a repeated field.
    // Start the ProtoMessageTranslator to translate the array.
    StartMessageTranslator();
  }
  translator_->Input().StartList(name);
  ++depth_;
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::EndList() {
  if (!status_.ok()) {
    // In error state - return right away
    return this;
  }
  --depth_;
  if (depth_ < 0) {
    status_ =
        pbutil::Status(pberr::INVALID_ARGUMENT, "Mismatched end of array.");
    return this;
  }
  if (depth_ == 0) {
    // We ended the root list, we're all done!
    done_ = true;
    return this;
  }
  translator_->Input().EndList();
  if (depth_ == 1) {
    // An element of the outermost array was closed - end the
    // ProtoMessageTranslator to save the translated message.
    EndMessageTranslator();
  }
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderBool(
    pb::StringPiece name, bool value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderBool(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderInt32(
    pb::StringPiece name, pb::int32 value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderInt32(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderUint32(
    pb::StringPiece name, pb::uint32 value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderUint32(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderInt64(
    pb::StringPiece name, pb::int64 value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderInt64(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderUint64(
    pb::StringPiece name, pb::uint64 value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderUint64(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderDouble(
    pb::StringPiece name, double value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderDouble(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderFloat(
    pb::StringPiece name, float value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderFloat(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderString(
    pb::StringPiece name, pb::StringPiece value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderString(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderBytes(
    pb::StringPiece name, pb::StringPiece value) {
  RenderData(name, [this, name, value]() {
    translator_->Input().RenderBytes(name, value);
  });
  return this;
}

RequestStreamTranslator* RequestStreamTranslator::RenderNull(
    pb::StringPiece name) {
  RenderData(name, [this, name]() { translator_->Input().RenderNull(name); });
  return this;
}

void RequestStreamTranslator::StartMessageTranslator() {
  RequestInfo request_info;
  request_info.message_type = request_info_.message_type;
  request_info.body_field_path = request_info_.body_field_path;
  // As we need to weave the variable bindings only for the first message, we
  // can use vector::swap() to avoid copying and to clear the bindings from
  // request_info_, s.t. the subsequent messages don't use them.
  request_info.variable_bindings.swap(request_info_.variable_bindings);
  // Create a RequestMessageTranslator to handle the events in a single message
  translator_.reset(new RequestMessageTranslator(
      type_resolver_, output_delimiters_, std::move(request_info)));
}

void RequestStreamTranslator::EndMessageTranslator() {
  if (!translator_->Status().ok()) {
    // Translation wasn't successful
    status_ = translator_->Status();
    return;
  }
  // Save the translated message and reset our state for the next one.
  std::string message;
  if (translator_->NextMessage(&message)) {
    messages_.emplace_back(std::move(message));
  } else {
    // This shouldn't happen unless something like StartList(), StartObject(),
    // EndList() has been called
    status_ = pbutil::Status(pberr::INVALID_ARGUMENT, "Invalid object");
  }
  translator_.reset();
}

void RequestStreamTranslator::RenderData(pb::StringPiece name,
                                         std::function<void()> renderer) {
  if (!status_.ok()) {
    // In error state - ignore
    return;
  }
  if (depth_ == 0) {
    // In depth_ == 0 case we expect only a StartList()
    status_ = pbutil::Status(pberr::INVALID_ARGUMENT,
                             "Expected an array instead of a scalar value.");
  } else if (depth_ == 1) {
    // This means we have an array of scalar values. This can happen if the HTTP
    // body is mapped to a scalar field.
    // We need to start the ProtoMessageTranslator, render the scalar value to
    // translate it and end the ProtoMessageTranslator to save the translated
    // message.
    StartMessageTranslator();
    renderer();
    EndMessageTranslator();
  } else {  // depth_ > 1
    renderer();
  }
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
