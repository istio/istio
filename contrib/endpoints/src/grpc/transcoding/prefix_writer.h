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
#ifndef GRPC_TRANSCODING_PREFIX_WRITER_H_
#define GRPC_TRANSCODING_PREFIX_WRITER_H_

#include <string>
#include <vector>

#include "google/protobuf/stubs/stringpiece.h"
#include "google/protobuf/util/internal/object_writer.h"

namespace google {
namespace api_manager {

namespace transcoding {

// PrefixWriter is helper ObjectWriter implementation that for each incoming
// object
//  1) writes the given prefix path by starting objects to the output
//     ObjectWriter,
//  2) forwards the writer events for a single object,
//  3) unwinds the prefix, by closing objects in the reverse order.
//
// E.g.
//
//  PrefixWriter pw("A.B.C", out);
//  pw.StartObject("Root");
//  ...
//  pw.RenderString("x", "value");
//  ...
//  pw.EndObject("Root");
//
// is equivalent to
//
//  out.StartObject("Root");
//  out.StartObject("A");
//  out.StartObject("B");
//  out.StartObject("C");
//  ...
//  pw.RenderString("x", "value");
//  ...
//  out.EndObject("C");
//  out.EndObject("B");
//  out.EndObject("A");
//  out.EndObject("Root");
//
class PrefixWriter : public google::protobuf::util::converter::ObjectWriter {
 public:
  // prefix is a '.' delimited prefix path to be added
  PrefixWriter(const std::string& prefix,
               google::protobuf::util::converter::ObjectWriter* ow);

  // ObjectWriter methods.
  PrefixWriter* StartObject(google::protobuf::StringPiece name);
  PrefixWriter* EndObject();
  PrefixWriter* StartList(google::protobuf::StringPiece name);
  PrefixWriter* EndList();
  PrefixWriter* RenderBool(google::protobuf::StringPiece name, bool value);
  PrefixWriter* RenderInt32(google::protobuf::StringPiece name,
                            google::protobuf::int32 value);
  PrefixWriter* RenderUint32(google::protobuf::StringPiece name,
                             google::protobuf::uint32 value);
  PrefixWriter* RenderInt64(google::protobuf::StringPiece name,
                            google::protobuf::int64 value);
  PrefixWriter* RenderUint64(google::protobuf::StringPiece name,
                             google::protobuf::uint64 value);
  PrefixWriter* RenderDouble(google::protobuf::StringPiece name, double value);
  PrefixWriter* RenderFloat(google::protobuf::StringPiece name, float value);
  PrefixWriter* RenderString(google::protobuf::StringPiece name,
                             google::protobuf::StringPiece value);
  PrefixWriter* RenderBytes(google::protobuf::StringPiece name,
                            google::protobuf::StringPiece value);
  PrefixWriter* RenderNull(google::protobuf::StringPiece name);

 private:
  // Helper method to start the prefix and return the name to use for the value.
  google::protobuf::StringPiece StartPrefix(google::protobuf::StringPiece name);

  // Helper method to end the prefix.
  void EndPrefix();

  // The path prefix if the HTTP body maps to a nested message in the proto.
  std::vector<std::string> prefix_;

  // Tracks the depth within the output, so we know when to write the prefix
  // and when to close it off.
  int non_actionable_depth_;

  // The output object writer to forward the writer events.
  google::protobuf::util::converter::ObjectWriter* writer_;

  PrefixWriter(const PrefixWriter&) = delete;
  PrefixWriter& operator=(const PrefixWriter&) = delete;
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_PREFIX_WRITER_H_
