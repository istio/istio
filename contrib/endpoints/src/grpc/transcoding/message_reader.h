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
#ifndef GRPC_TRANSCODING_MESSAGE_READER_H_
#define GRPC_TRANSCODING_MESSAGE_READER_H_

#include <memory>

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/stubs/status.h"

namespace google {
namespace api_manager {

namespace transcoding {

// MessageReader helps extract full messages from a ZeroCopyInputStream of
// messages in gRPC wire format (http://www.grpc.io/docs/guides/wire.html). Each
// message is returned in a ZeroCopyInputStream. MessageReader doesn't advance
// the underlying ZeroCopyInputStream unless there is a full message available.
// This is done to avoid copying while buffering.
//
// Example:
//   MessageReader reader(&input);
//
//   while (!reader.Finished()) {
//     auto message = reader.NextMessage();
//     if (!message) {
//       // No message is available at this moment.
//       break;
//     }
//
//     const void* buffer = nullptr;
//     int size = 0;
//     while (message.Next(&buffer, &size)) {
//       // Process the message data.
//       ...
//     }
//   }
//
// NOTE: MesssageReader assumes that ZeroCopyInputStream::ByteCount() returns
//       the number of bytes available to read at the moment. That's what
//       MessageReader uses to determine whether there is a complete message
//       available or not.
//
// NOTE: MessageReader is unable to recognize the case when there is an
//       incomplete message at the end of the input. The callers will need to
//       detect it and act appropriately.
//       This is because the MessageReader doesn't call Next() on the input
//       stream until there is a full message available. So, if there is an
//       incomplete message at the end of the input, MessageReader won't call
//       Next() and won't know that the stream has finished.
//
class MessageReader {
 public:
  MessageReader(::google::protobuf::io::ZeroCopyInputStream* in);

  // If a full message is available, NextMessage() returns a ZeroCopyInputStream
  // over the message. Otherwise returns nullptr - this might be temporary, the
  // caller can call NextMessage() again later to check.
  // NOTE: the caller must consume the entire message before calling
  //       NextMessage() again.
  //       That's because the returned ZeroCopyInputStream is a wrapper on top
  //       of the original ZeroCopyInputStream and the MessageReader relies on
  //       the caller to advance the stream to the next message before calling
  //       NextMessage() again.
  std::unique_ptr<::google::protobuf::io::ZeroCopyInputStream> NextMessage();

  // Returns true if the stream has ended (this is permanent); otherwise returns
  // false.
  bool Finished() const { return finished_; }

 private:
  ::google::protobuf::io::ZeroCopyInputStream* in_;
  // The size of the current message.
  unsigned int current_message_size_;
  // Whether we have read the current message size or not
  bool have_current_message_size_;
  // Are we all done?
  bool finished_;

  MessageReader(const MessageReader&) = delete;
  MessageReader& operator=(const MessageReader&) = delete;
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_MESSAGE_READER_H_
