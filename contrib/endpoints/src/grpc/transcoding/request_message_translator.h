/*
 * Copyright (C) Extensible Service Proxy Authors
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above copyright
 *    notice, this list of conditions and the following disclaimer in the
 *    documentation and/or other materials provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
 * ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 * IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED.  IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE
 * FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 * DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
 * LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
 * OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
 * SUCH DAMAGE.
 */
#ifndef GRPC_TRANSCODING_REQUEST_MESSAGE_TRANSLATOR_H_
#define GRPC_TRANSCODING_REQUEST_MESSAGE_TRANSLATOR_H_

#include <memory>
#include <string>

#include "google/protobuf/stubs/bytestream.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/error_listener.h"
#include "google/protobuf/util/internal/protostream_objectwriter.h"
#include "google/protobuf/util/type_resolver.h"
#include "src/grpc/transcoding/message_stream.h"
#include "src/grpc/transcoding/prefix_writer.h"
#include "src/grpc/transcoding/request_weaver.h"

namespace google {
namespace api_manager {

namespace transcoding {

// RequestInfo contains the information needed for request translation.
struct RequestInfo {
  // The protobuf type that we are translating to.
  const google::protobuf::Type* message_type;

  // body_field_path is a dot-delimited chain of protobuf field names that
  // defines the (potentially nested) location in the message, where the
  // translated HTTP body must be inserted. E.g. "shelf.theme" means that the
  // translated HTTP body must be inserted into the "theme" field of the "shelf"
  // field of the request message.
  std::string body_field_path;

  // A collection of variable bindings extracted from the HTTP url or other
  // sources that must be injected into certain fields of the translated
  // message.
  std::vector<RequestWeaver::BindingInfo> variable_bindings;
};

// RequestMessageTranslator translates ObjectWriter events into a single
// protobuf message. The protobuf message is built based on the input
// ObjectWriter events and a RequestInfo.
// If output_delimiter is true, RequestMessageTranslator will prepend the output
// message with a GRPC message delimiter - a 1-byte compression flag and a
// 4-byte message length (see http://www.grpc.io/docs/guides/wire.html).
// The translated message is exposed through MessageStream interface.
//
// The implementation uses a pipeline of ObjectWriters to do the job:
//  PrefixWriter -> RequestWeaver -> ProtoStreamObjectWriter
//
//  - PrefixWriter writes the body prefix making sure that the body goes to the
//    right place and forwards the writer events to the RequestWeaver. This link
//    will be absent if the prefix is empty.
//  - RequestWeaver injects the variable bindings and forwards the writer events
//    to the ProtoStreamObjectWriter. This link will be absent if there are no
//    variable bindings to weave.
//  - ProtoStreamObjectWriter does the actual proto writing.
//
// Example:
//   RequestMessageTranslator t(type_resolver, true, std::move(request_info));
//
//   ObjectWriter& input = t.Input();
//
//   input.StartObject("");
//   ...
//   write the request body using input ObjectWriter
//   ...
//   input.EndObject();
//
//   if (!t.Status().ok()) {
//     printf("Error: %s\n", t->Status().ErrorMessage().as_string().c_str());
//     return;
//   }
//
//   std::string message;
//   if (t.NextMessage(&message)) {
//     printf("Message=%s\n", message.c_str());
//   }
//
class RequestMessageTranslator : public MessageStream {
 public:
  // type_resolver is forwarded to the ProtoStreamObjectWriter that does the
  // actual proto writing.
  // output_delimiter specifies whether to output the GRPC 5 byte message
  // delimiter before the message or not.
  RequestMessageTranslator(google::protobuf::util::TypeResolver& type_resolver,
                           bool output_delimiter, RequestInfo request_info);

  ~RequestMessageTranslator();

  // An ObjectWriter that takes the input object to translate
  google::protobuf::util::converter::ObjectWriter& Input() {
    return *writer_pipeline_;
  }

  // MessageStream methods
  bool NextMessage(std::string* message);
  bool Finished() const;
  google::protobuf::util::Status Status() const {
    return error_listener_.status();
  }

 private:
  // Reserves space (5 bytes) for the GRPC delimiter to be written later. As it
  // requires the length of the message, we can't write it before the message
  // itself.
  void ReserveDelimiterSpace();

  // Writes the wire delimiter into the reserved delimiter space at the begining
  // of this->message_.
  void WriteDelimiter();

  // The message being written
  std::string message_;

  // StringByteSink instance that appends the bytes to this->message_. We pass
  // this to the ProtoStreamObjectWriter for writing the translated message.
  google::protobuf::strings::StringByteSink sink_;

  // StatusErrorListener converts the error events into a Status
  class StatusErrorListener
      : public ::google::protobuf::util::converter::ErrorListener {
   public:
    StatusErrorListener() : status_(::google::protobuf::util::Status::OK) {}
    virtual ~StatusErrorListener() {}

    ::google::protobuf::util::Status status() const { return status_; }

    // ErrorListener implementation
    void InvalidName(
        const ::google::protobuf::util::converter::LocationTrackerInterface&
            loc,
        ::google::protobuf::StringPiece unknown_name,
        ::google::protobuf::StringPiece message);
    void InvalidValue(
        const ::google::protobuf::util::converter::LocationTrackerInterface&
            loc,
        ::google::protobuf::StringPiece type_name,
        ::google::protobuf::StringPiece value);
    void MissingField(
        const ::google::protobuf::util::converter::LocationTrackerInterface&
            loc,
        ::google::protobuf::StringPiece missing_name);

   private:
    ::google::protobuf::util::Status status_;

    GOOGLE_DISALLOW_EVIL_CONSTRUCTORS(StatusErrorListener);
  };

  // ErrorListener implementation that converts the error events into
  // a status.
  StatusErrorListener error_listener_;

  // The proto writer for writing the actual proto bytes
  google::protobuf::util::converter::ProtoStreamObjectWriter proto_writer_;

  // A RequestWeaver for writing the variable bindings
  std::unique_ptr<RequestWeaver> request_weaver_;

  // A PrefixWriter for writing the body prefix
  std::unique_ptr<PrefixWriter> prefix_writer_;

  // The ObjectWriter that will receive the events
  // This is either &proto_writer_, request_weaver_.get() or
  // prefix_writer_.get()
  google::protobuf::util::converter::ObjectWriter* writer_pipeline_;

  // Whether to ouput a delimiter before the message or not
  bool output_delimiter_;

  // A flag that indicates whether the message has been already read or not
  // This helps with the MessageStream implementation.
  bool finished_;

  // GRPC delimiter size = 1 + 4 - 1-byte compression flag and 4-byte message
  // length.
  static const int kDelimiterSize = 5;

  RequestMessageTranslator(const RequestMessageTranslator&) = delete;
  RequestMessageTranslator& operator=(const RequestMessageTranslator&) = delete;
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_REQUEST_MESSAGE_TRANSLATOR_H_
