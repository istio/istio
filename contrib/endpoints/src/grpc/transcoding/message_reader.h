/* Copyright 2016 Google Inc. All Rights Reserved.
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
#ifndef GRPC_TRANSCODING_MESSAGE_READER_H_
#define GRPC_TRANSCODING_MESSAGE_READER_H_

#include <memory>

#include "contrib/endpoints/src/grpc/transcoding/transcoder_input_stream.h"
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
  MessageReader(TranscoderInputStream* in);

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
  TranscoderInputStream* in_;
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
