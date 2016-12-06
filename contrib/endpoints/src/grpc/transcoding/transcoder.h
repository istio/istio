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
#ifndef GRPC_TRANSCODING_TRANSCODER_H_
#define GRPC_TRANSCODING_TRANSCODER_H_

#include "google/protobuf/io/zero_copy_stream.h"
#include "google/protobuf/stubs/status.h"

namespace google {
namespace api_manager {
namespace transcoding {

// Transcoder interface that transcodes a single request. It holds
//  - translated request stream,
//  - status of request translation,
//  - translated response stream,
//  - status of response translation.
//
// NOTE: Transcoder uses ::google::protobuf::io::ZeroCopyInputStream for
//       carrying the payloads both for input and output. It assumes the
//       following interpretation of the ZeroCopyInputStream interface:
//
// bool ZeroCopyInputStream::Next(const void** data, int* size);
//
// Obtains a chunk of data from the stream.
//
// Preconditions:
// * "size" and "data" are not NULL.
//
// Postconditions:
// * If the returned value is false, there is no more data to return or an error
//   occurred.  This is permanent.
// * Otherwise, "size" points to the actual number of bytes read and "data"
//   points to a pointer to a buffer containing these bytes.
// * Ownership of this buffer remains with the stream, and the buffer remains
//   valid only until some other method of the stream is called or the stream is
//   destroyed.
// * It is legal for the returned buffer to have zero size. That means there is
//   no data available at this point. This is temporary. The caller needs to try
//   again later.
//
//
// void ZeroCopyInputStream::BackUp(int count);
//
// Backs up a number of bytes, so that the next call to Next() returns
// data again that was already returned by the last call to Next().  This
// is useful when writing procedures that are only supposed to read up
// to a certain point in the input, then return.  If Next() returns a
// buffer that goes beyond what you wanted to read, you can use BackUp()
// to return to the point where you intended to finish.
//
// Preconditions:
// * The last method called must have been Next().
// * count must be less than or equal to the size of the last buffer
//   returned by Next().
//
// Postconditions:
// * The last "count" bytes of the last buffer returned by Next() will be
//   pushed back into the stream.  Subsequent calls to Next() will return
//   the same data again before producing new data.
//
//
// bool ZeroCopyInputStream::Skip(int count);
//
// Not used and not implemented by the Transcoder.
//
//
// int64 ZeroCopyInputStream::ByteCount() const;
//
// Returns the number of bytes available for reading at this moment
//
//
// NOTE: To support flow-control the translation & reading the input stream
//       happens on-demand in both directions. I.e. Transcoder doesn't call
//       Next() on the input stream unless Next() is called on the output stream
//       and it ran out of input to translate.
//
// EXAMPLE:
//   Transcoder* t = transcoder_factory->Create(...);
//
//   const void* buffer = nullptr;
//   int size = 0;
//   while (backend can accept request) {
//     if (!t->RequestOutput()->Next(&buffer, &size)) {
//       // end of input or error
//       if (t->RequestStatus().ok()) {
//         // half-close the request
//       } else {
//         // error
//       }
//     } else if (size == 0) {
//       // no transcoded request data available at this point; wait for more
//       // request data to arrive and run this loop again later.
//       break;
//     } else {
//       // send the buffer to the backend
//       ...
//     }
//   }
//
//   const void* buffer = nullptr;
//   int size = 0;
//   while (client can accept response) {
//     if (!t->ResponseOutput()->Next(&buffer, &size)) {
//       // end of input or error
//       if (t->ResponseStatus().ok()) {
//         // close the request
//       } else {
//         // error
//       }
//     } else if (size == 0) {
//       // no transcoded response data available at this point; wait for more
//       // response data to arrive and run this loop again later.
//       break;
//     } else {
//       // send the buffer to the client
//       ...
//     }
//   }
//
class Transcoder {
 public:
  // ZeroCopyInputStream to read the transcoded request.
  virtual ::google::protobuf::io::ZeroCopyInputStream* RequestOutput() = 0;

  // The status of request transcoding
  virtual ::google::protobuf::util::Status RequestStatus() = 0;

  // ZeroCopyInputStream to read the transcoded response.
  virtual ::google::protobuf::io::ZeroCopyInputStream* ResponseOutput() = 0;

  // The status of response transcoding
  virtual ::google::protobuf::util::Status ResponseStatus() = 0;

  // Virtual destructor
  virtual ~Transcoder() {}
};

}  // namespace transcoding
}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODER_H_
