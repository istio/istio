// Copyright (C) Extensible Service Proxy Authors
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
////////////////////////////////////////////////////////////////////////////////
//
#include "src/grpc/transcoding/request_message_translator.h"

#include <string>

#include "google/protobuf/stubs/bytestream.h"
#include "google/protobuf/util/internal/error_listener.h"
#include "google/protobuf/util/internal/protostream_objectwriter.h"
#include "src/grpc/transcoding/prefix_writer.h"
#include "src/grpc/transcoding/request_weaver.h"

namespace pb = ::google::protobuf;
namespace pbutil = ::google::protobuf::util;
namespace pbconv = ::google::protobuf::util::converter;

namespace google {
namespace api_manager {

namespace transcoding {

namespace {

pbconv::ProtoStreamObjectWriter::Options GetProtoWriterOptions() {
  auto options = pbconv::ProtoStreamObjectWriter::Options::Defaults();
  // Don't fail the translation if there are unknown fields in JSON.
  // This will make sure that we allow backward and forward compatible APIs.
  options.ignore_unknown_fields = true;
  return options;
}

}  // namespace

RequestMessageTranslator::RequestMessageTranslator(
    google::protobuf::util::TypeResolver& type_resolver, bool output_delimiter,
    RequestInfo request_info)
    : message_(),
      sink_(&message_),
      error_listener_(),
      proto_writer_(&type_resolver, *request_info.message_type, &sink_,
                    &error_listener_, GetProtoWriterOptions()),
      request_weaver_(),
      prefix_writer_(),
      writer_pipeline_(&proto_writer_),
      output_delimiter_(output_delimiter),
      finished_(false) {
  // Create a RequestWeaver if we have variable bindings to weave
  if (!request_info.variable_bindings.empty()) {
    request_weaver_.reset(new RequestWeaver(
        std::move(request_info.variable_bindings), writer_pipeline_));
    writer_pipeline_ = request_weaver_.get();
  }

  // Create a PrefixWriter if there is a prefix to write
  if (!request_info.body_field_path.empty() &&
      "*" != request_info.body_field_path) {
    prefix_writer_.reset(
        new PrefixWriter(request_info.body_field_path, writer_pipeline_));
    writer_pipeline_ = prefix_writer_.get();
  }

  if (output_delimiter_) {
    // Reserve space for the delimiter at the begining of the message_
    ReserveDelimiterSpace();
  }
}

RequestMessageTranslator::~RequestMessageTranslator() {}

bool RequestMessageTranslator::Finished() const { return finished_; }

bool RequestMessageTranslator::NextMessage(std::string* message) {
  if (Finished()) {
    // Finished reading
    return false;
  }
  if (!proto_writer_.done()) {
    // No full message yet
    return false;
  }
  if (output_delimiter_) {
    WriteDelimiter();
  }
  *message = std::move(message_);
  finished_ = true;
  return true;
}

void RequestMessageTranslator::ReserveDelimiterSpace() {
  static char reserved[kDelimiterSize] = {0};
  sink_.Append(reserved, sizeof(reserved));
}

namespace {

void SizeToDelimiter(unsigned size, unsigned char* delimiter) {
  delimiter[0] = 0;  // compression bit

  // big-endian 32-bit length
  delimiter[4] = 0xFF & size;
  size >>= 8;
  delimiter[3] = 0xFF & size;
  size >>= 8;
  delimiter[2] = 0xFF & size;
  size >>= 8;
  delimiter[1] = 0xFF & size;
}

}  // namespace

void RequestMessageTranslator::WriteDelimiter() {
  // Asumming that the message_.size() - kDelimiterSize is less than UINT_MAX
  SizeToDelimiter(static_cast<unsigned>(message_.size() - kDelimiterSize),
                  reinterpret_cast<unsigned char*>(&message_[0]));
}

void RequestMessageTranslator::StatusErrorListener::InvalidName(
    const ::google::protobuf::util::converter::LocationTrackerInterface& loc,
    ::google::protobuf::StringPiece unknown_name,
    ::google::protobuf::StringPiece message) {
  status_ = ::google::protobuf::util::Status(
      ::google::protobuf::util::error::INVALID_ARGUMENT,
      loc.ToString() + ": " + message.ToString());
}

void RequestMessageTranslator::StatusErrorListener::InvalidValue(
    const ::google::protobuf::util::converter::LocationTrackerInterface& loc,
    ::google::protobuf::StringPiece type_name,
    ::google::protobuf::StringPiece value) {
  status_ = ::google::protobuf::util::Status(
      ::google::protobuf::util::error::INVALID_ARGUMENT,
      loc.ToString() + ": invalid value " + value.ToString() + " for type " +
          type_name.ToString());
}

void RequestMessageTranslator::StatusErrorListener::MissingField(
    const ::google::protobuf::util::converter::LocationTrackerInterface& loc,
    ::google::protobuf::StringPiece missing_name) {
  status_ = ::google::protobuf::util::Status(
      ::google::protobuf::util::error::INVALID_ARGUMENT,
      loc.ToString() + ": missing field " + missing_name.ToString());
}

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
