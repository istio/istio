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
#ifndef GRPC_TRANSCODING_MESSAGE_STREAM_H_
#define GRPC_TRANSCODING_MESSAGE_STREAM_H_

#include <memory>
#include <string>

#include "contrib/endpoints/src/grpc/transcoding/transcoder_input_stream.h"
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
  std::unique_ptr<TranscoderInputStream> CreateInputStream();
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_MESSAGE_STREAM_H_
