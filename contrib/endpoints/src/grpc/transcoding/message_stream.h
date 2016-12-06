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
#ifndef GRPC_TRANSCODING_MESSAGE_STREAM_H_
#define GRPC_TRANSCODING_MESSAGE_STREAM_H_

#include <memory>
#include <string>

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/stubs/status.h"

namespace google {
namespace api_manager {

namespace transcoding {

// MessageStream abstracts a stream of std::string represented messages.  Among
// other things MessageStream helps us to reuse some code for streaming and
// non-streaming implementations of request translation.
// We'll use this simple interface internally in transcoding (ESP) and will
// implement ZeroCopyInputStream based on this for exposing it to Nginx
// integration code (or potentially other consumers of transcoding).
//
// Example:
//  MessageStream& stream = GetTranslatorStream();
//
//  if (!stream.Status().ok()) {
//    printf("Error - %s", stream.Status().error_message().c_str());
//    return;
//  }
//
//  std::string message;
//  while (stream.Message(&message)) {
//    printf("Message=%s\n", message.c_str());
//  }
//
//  if (stream.Finished()) {
//    printf("Finished\n");
//  } else {
//    printf("No messages at this time. Try again later \n");
//  }
//
// Unlike ZeroCopyInputStream this interface doesn't support features like
// BackUp(), Skip() and is easier to implement. At the same time the
// implementations can achieve "zero copy" by moving the std::string messages.
// However, this assumes a particular implementation (std::string based), so
// we use it only internally.
//
class MessageStream {
 public:
  // Retrieves the next message from the stream if there is one available.
  // The implementation can use move assignment operator to avoid a copy.
  // If no message is available at this time, returns false (this might be
  // temporary); otherwise returns true.
  virtual bool NextMessage(std::string* message) = 0;
  // Returns true if no messages are left (this is permanent); otherwise return
  // false.
  virtual bool Finished() const = 0;
  // Stream status to report errors
  virtual ::google::protobuf::util::Status Status() const = 0;
  // Virtual destructor
  virtual ~MessageStream() {}
  // Creates ZeroCopyInputStream implementation based on this stream
  std::unique_ptr<::google::protobuf::io::ZeroCopyInputStream>
  CreateZeroCopyInputStream();
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_MESSAGE_STREAM_H_
