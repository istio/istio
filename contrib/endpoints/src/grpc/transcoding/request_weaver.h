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
#ifndef GRPC_TRANSCODING_REQUEST_WEAVER_H_
#define GRPC_TRANSCODING_REQUEST_WEAVER_H_

#include <list>
#include <stack>
#include <string>
#include <vector>

#include "google/protobuf/stubs/stringpiece.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/object_writer.h"

namespace google {
namespace api_manager {

namespace transcoding {

// RequestWeaver is an ObjectWriter implementation that weaves-in given variable
// bindings together with the input ObjectWriter events and forwards it to the
// output ObjectWriter specified in the constructor.
//
// E.g., assume we have the {"shelf.theme" -> "Russian Classics"} binding and
// the caller is "writing" an object calling the weaver methods as follows:
//
//   weaver.StartObject("");
//   ...
//   weaver.StartObject("shelf");
//   weaver.RenderString("name", "1");
//   weaver.EndObject();
//   ...
//   weaver.EndObject();
//
// The request weaver will forward all these events to the output ObjectWriter
// and will also inject the "shelf.theme" value:
//
//   out.StartObject("");
//   ...
//   out.StartObject("shelf");
//   out.RenderString("name", "1");
//   out.RenderString("theme", "Russian Classics"); <-- weaved value
//   out.EndObject();
//   ...
//   out.EndObject();
//
class RequestWeaver : public google::protobuf::util::converter::ObjectWriter {
 public:
  // a single binding to be weaved-in into the message
  struct BindingInfo {
    // field_path is a chain of protobuf fields that defines the (potentially
    // nested) location in the message, where the value should be weaved-in.
    // E.g. {"shelf", "theme"} field_path means that the value should be
    // inserted into the "theme" field of the "shelf" field of the request
    // message.
    std::vector<const google::protobuf::Field*> field_path;
    std::string value;
  };

  // We accept 'bindings' by value to enable moving if the caller doesn't need
  // the passed object anymore.
  // RequestWeaver does not take the ownership of 'ow'. The caller must make
  // sure that it exists throughout the lifetime of the RequestWeaver.
  RequestWeaver(std::vector<BindingInfo> bindings,
                google::protobuf::util::converter::ObjectWriter* ow);

  // ObjectWriter methods
  RequestWeaver* StartObject(google::protobuf::StringPiece name);
  RequestWeaver* EndObject();
  RequestWeaver* StartList(google::protobuf::StringPiece name);
  RequestWeaver* EndList();
  RequestWeaver* RenderBool(google::protobuf::StringPiece name, bool value);
  RequestWeaver* RenderInt32(google::protobuf::StringPiece name,
                             google::protobuf::int32 value);
  RequestWeaver* RenderUint32(google::protobuf::StringPiece name,
                              google::protobuf::uint32 value);
  RequestWeaver* RenderInt64(google::protobuf::StringPiece name,
                             google::protobuf::int64 value);
  RequestWeaver* RenderUint64(google::protobuf::StringPiece name,
                              google::protobuf::uint64 value);
  RequestWeaver* RenderDouble(google::protobuf::StringPiece name, double value);
  RequestWeaver* RenderFloat(google::protobuf::StringPiece name, float value);
  RequestWeaver* RenderString(google::protobuf::StringPiece name,
                              google::protobuf::StringPiece value);
  RequestWeaver* RenderNull(google::protobuf::StringPiece name);
  RequestWeaver* RenderBytes(google::protobuf::StringPiece name,
                             google::protobuf::StringPiece value);

 private:
  // Container for information to be weaved.
  // WeaveInfo represents an internal node in the weave tree.
  //   messages: list of non-leaf children nodes.
  //   bindings: list of binding values (leaf nodes) in this node.
  struct WeaveInfo {
    // NOTE: using list instead of map/unordered_map as the number of keys is
    // going to be small.
    std::list<std::pair<const google::protobuf::Field*, WeaveInfo>> messages;
    std::list<std::pair<const google::protobuf::Field*, std::string>> bindings;

    // Find the entry for the speciied field in messages list .
    WeaveInfo* FindWeaveMsg(google::protobuf::StringPiece field_name);

    // Create an entry in messages for the given field. The caller must make
    // sure that there is no existing entry for the same field before calling.
    WeaveInfo* CreateWeaveMsg(const google::protobuf::Field* field);

    // Ensure that there is an entry for the given field and return it.
    WeaveInfo* FindOrCreateWeaveMsg(const google::protobuf::Field* field);
  };

  // Bind value to location indicated by fields.
  void Bind(std::vector<const google::protobuf::Field*> field_path,
            std::string value);

  // Write out the whole subtree rooted at info to the ProtoStreamObjectWriter.
  void WeaveTree(WeaveInfo* info);

  // Checks if any repeated fields with the same field name are in the current
  // node of the weave tree. Output them if there are any.
  void CollisionCheck(google::protobuf::StringPiece name);

  // All the headers, variable bindings and parameter bindings to be weaved in.
  //   root_   : root of the tree to be weaved in.
  //   current_: stack of nodes in the current visit path from the root.
  // NOTE: current_ points to the nodes owned by root_. It doesn't maintain the
  // ownership itself.
  WeaveInfo root_;
  std::stack<WeaveInfo*> current_;

  // Destination ObjectWriter for final output.
  google::protobuf::util::converter::ObjectWriter* ow_;

  // Counter for number of uninteresting nested messages.
  int non_actionable_depth_;

  RequestWeaver(const RequestWeaver&) = delete;
  RequestWeaver& operator=(const RequestWeaver&) = delete;
};

}  // namespace transcoding

}  // namespace api_manager
}  // namespace google

#endif  // API_MANAGER_TRANSCODING_REQUEST_WEAVER_H_
