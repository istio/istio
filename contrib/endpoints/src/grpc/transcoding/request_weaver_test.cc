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
#include "src/grpc/transcoding/request_weaver.h"

#include <memory>
#include <string>
#include <vector>

#include "google/protobuf/stubs/strutil.h"
#include "google/protobuf/type.pb.h"
#include "google/protobuf/util/internal/expecting_objectwriter.h"
#include "gtest/gtest.h"

namespace google {
namespace api_manager {

namespace transcoding {
namespace testing {
namespace {

using google::protobuf::Field;
using ::testing::InSequence;

class RequestWeaverTest : public ::testing::Test {
 protected:
  RequestWeaverTest() : mock_(), expect_(&mock_) {}

  void Bind(std::string field_path_str, std::string value) {
    auto field_names = google::protobuf::Split(field_path_str, ".");
    std::vector<const Field*> field_path;
    for (const auto& n : field_names) {
      fields_.emplace_back(CreateField(n));
      field_path.emplace_back(&fields_.back());
    }
    bindings_.emplace_back(
        RequestWeaver::BindingInfo{field_path, std::move(value)});
  }

  std::unique_ptr<RequestWeaver> Create() {
    return std::unique_ptr<RequestWeaver>(
        new RequestWeaver(std::move(bindings_), &mock_));
  }

  google::protobuf::util::converter::MockObjectWriter mock_;
  google::protobuf::util::converter::ExpectingObjectWriter expect_;
  InSequence seq_;  // all our expectations must be ordered

 private:
  std::vector<RequestWeaver::BindingInfo> bindings_;
  std::list<Field> fields_;

  Field CreateField(google::protobuf::StringPiece name) {
    Field::Cardinality card;
    if (name.ends_with("*")) {
      // we use "*" at the end of the field name to denote a repeated field.
      card = Field::CARDINALITY_REPEATED;
      name.remove_suffix(1);
    } else {
      card = Field::CARDINALITY_OPTIONAL;
    }
    Field field;
    field.set_name(name);
    field.set_kind(Field::TYPE_STRING);
    field.set_cardinality(card);
    field.set_number(1);  // dummy number
    return field;
  }
};

TEST_F(RequestWeaverTest, PassThrough) {
  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "a");
  expect_.RenderBytes("by", "b");
  expect_.RenderInt32("i", 1);
  expect_.RenderUint32("ui", 2);
  expect_.RenderInt64("i64", 3);
  expect_.RenderUint64("ui64", 4);
  expect_.RenderBool("b", true);
  expect_.RenderNull("null");
  expect_.StartObject("B");
  expect_.RenderString("y", "b");
  expect_.EndObject();  // B
  expect_.EndObject();  // A
  expect_.EndObject();  // ""

  auto w = Create();
  w->StartObject("");
  w->StartObject("A");
  w->RenderString("x", "a");
  w->RenderBytes("by", "b");
  w->RenderInt32("i", google::protobuf::int32(1));
  w->RenderUint32("ui", google::protobuf::uint32(2));
  w->RenderInt64("i64", google::protobuf::int64(3));
  w->RenderUint64("ui64", google::protobuf::uint64(4));
  w->RenderBool("b", true);
  w->RenderNull("null");
  w->StartObject("B");
  w->RenderString("y", "b");
  w->EndObject();
  w->EndObject();
  w->EndObject();
}

TEST_F(RequestWeaverTest, Level0Bindings) {
  Bind("_x", "a");
  Bind("_y", "b");
  Bind("_z", "c");

  // {
  //   "i" : "10",
  //   "x" : "d",
  //   ("_x" : "a",)
  //   ("_y" : "b",)
  //   ("_z" : "c",)
  // }

  expect_.StartObject("");
  expect_.RenderInt32("i", 10);
  expect_.RenderString("x", "d");
  expect_.RenderString("_x", "a");
  expect_.RenderString("_y", "b");
  expect_.RenderString("_z", "c");
  expect_.EndObject();

  auto w = Create();

  w->StartObject("");
  w->RenderInt32("i", 10);
  w->RenderString("x", "d");
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, Level1Bindings) {
  Bind("A._x", "a");
  Bind("A._y", "b");
  Bind("B._x", "c");

  // {
  //   "x" : "d",
  //   "A" : {
  //     "y" : "e",
  //     ("_x" : "a"),
  //     ("_y" : "b",)
  //   }
  //   "B" : {
  //     "z" : "f",
  //     ("_x" : "c", )
  //   }
  // }

  expect_.StartObject("");
  expect_.RenderString("x", "d");
  expect_.StartObject("A");
  expect_.RenderString("y", "e");
  expect_.RenderString("_x", "a");
  expect_.RenderString("_y", "b");
  expect_.EndObject();  // A
  expect_.StartObject("B");
  expect_.RenderString("z", "f");
  expect_.RenderString("_x", "c");
  expect_.EndObject();  // B
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->RenderString("x", "d");
  w->StartObject("A");
  w->RenderString("y", "e");
  w->EndObject();  // A
  w->StartObject("B");
  w->RenderString("z", "f");
  w->EndObject();  // B
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, Level2Bindings) {
  Bind("A.B._x", "a");
  Bind("A.C._y", "b");
  Bind("D.E._x", "c");

  // {
  //   "A" : {
  //     "B" : {
  //       "x" : "d",
  //       ("_x" : "a",)
  //     },
  //     "y" : "e",
  //     "C" : {
  //       ("_y" : "b",)
  //     }
  //   }
  //   "D" : {
  //     "z" : "f",
  //     "E" : {
  //       "u" : "g",
  //       ("_x" : "c",)
  //     },
  //   }
  // }
  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.StartObject("B");
  expect_.RenderString("x", "d");
  expect_.RenderString("_x", "a");
  expect_.EndObject();  // "B"
  expect_.RenderString("y", "e");
  expect_.StartObject("C");
  expect_.RenderString("_y", "b");
  expect_.EndObject();  // "C"
  expect_.EndObject();  // "A"
  expect_.StartObject("D");
  expect_.RenderString("z", "f");
  expect_.StartObject("E");
  expect_.RenderString("u", "g");
  expect_.RenderString("_x", "c");
  expect_.EndObject();  // "E"
  expect_.EndObject();  // "D"
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->StartObject("A");
  w->StartObject("B");
  w->RenderString("x", "d");
  w->EndObject();  // "B"
  w->RenderString("y", "e");
  w->StartObject("C");
  w->EndObject();  // "C"
  w->EndObject();  // "A"
  w->StartObject("D");
  w->RenderString("z", "f");
  w->StartObject("E");
  w->RenderString("u", "g");
  w->EndObject();  // "E"
  w->EndObject();  // "D"
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, Level2WeaveNewSubTree) {
  Bind("A.B._x", "a");

  // {
  //   "x" : "b",
  //   "C" : {
  //     "y" : "c",
  //     "D" : {
  //       "z" : "c",
  //     }
  //   },
  //   (
  //   "A" {
  //     "B" {
  //      "_x" : "a"
  //     }
  //   }
  //   )
  // }

  expect_.StartObject("");
  expect_.RenderString("x", "b");
  expect_.StartObject("C");
  expect_.RenderString("y", "c");
  expect_.StartObject("D");
  expect_.RenderString("z", "d");
  expect_.EndObject();  // "C"
  expect_.EndObject();  // "D"
  expect_.StartObject("A");
  expect_.StartObject("B");
  expect_.RenderString("_x", "a");
  expect_.EndObject();  // "B"
  expect_.EndObject();  // "A"
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->RenderString("x", "b");
  w->StartObject("C");
  w->RenderString("y", "c");
  w->StartObject("D");
  w->RenderString("z", "d");
  w->EndObject();  // "C"
  w->EndObject();  // "D"
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, MixedBindings) {
  Bind("_x", "a");
  Bind("A.B._y", "b");
  Bind("A._z", "c");

  // {
  //   "A" : {
  //     "x" : "d",
  //     "B" : {
  //       "y" : "e",
  //       ("_y" : "b",)
  //     },
  //     ("_z" : "c",)
  //   },
  //   ("_x" : "a",)
  // }

  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "d");
  expect_.StartObject("B");
  expect_.RenderString("y", "e");
  expect_.RenderString("_y", "b");
  expect_.EndObject();  // "B"
  expect_.RenderString("_z", "c");
  expect_.EndObject();  // "A"
  expect_.RenderString("_x", "a");
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->StartObject("A");
  w->RenderString("x", "d");
  w->StartObject("B");
  w->RenderString("y", "e");
  w->EndObject();  // "B"
  w->EndObject();  // "A"
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, MoreMixedBindings) {
  Bind("_x", "a");
  Bind("A._y", "b");
  Bind("B._z", "c");
  Bind("C.D._u", "d");

  // {
  //   "A" : {
  //     "x" : "d",
  //     ("_y" : "b",)
  //   },
  //   "B" : {
  //     "y" : "e",
  //     ("_z" : "c",)
  //   },
  //   ("_x" : "a",)
  //   (
  //   "C" : {
  //     "D" : {
  //       ("_u" : "d",)
  //     },
  //   },
  //   )
  // }

  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "d");
  expect_.RenderString("_y", "b");
  expect_.EndObject();  // "A"
  expect_.StartObject("B");
  expect_.RenderString("y", "e");
  expect_.RenderString("_z", "c");
  expect_.EndObject();  // "B"
  expect_.RenderString("_x", "a");
  expect_.StartObject("C");
  expect_.StartObject("D");
  expect_.RenderString("_u", "d");
  expect_.EndObject();  // "D"
  expect_.EndObject();  // "C"
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->StartObject("A");
  w->RenderString("x", "d");
  w->EndObject();  // "A"
  w->StartObject("B");
  w->RenderString("y", "e");
  w->EndObject();  // "B"
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, CollisionIgnored) {
  Bind("A.x", "a");

  // {
  //   "A" : {
  //     "x" : "b",
  //     ("x" : "a") -- ignored
  //   }
  // }

  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "b");
  expect_.EndObject();  // "A"
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->StartObject("A");
  w->RenderString("x", "b");
  w->EndObject();  // "A"
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, CollisionRepeated) {
  // "x*" means a repeated field with the name "x"
  Bind("A.x*", "b");
  Bind("A.x*", "c");
  Bind("A.x*", "d");

  // {
  //   "A" : {
  //     "x" : "a",
  //     ("x" : "b")
  //     ("x" : "c")
  //     ("x" : "d")
  //   }
  // }

  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "b");
  expect_.RenderString("x", "c");
  expect_.RenderString("x", "d");
  expect_.RenderString("x", "a");
  expect_.EndObject();  // "A"
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->StartObject("A");
  w->RenderString("x", "a");
  w->EndObject();  // "A"
  w->EndObject();  // ""
}

TEST_F(RequestWeaverTest, IgnoreListTest) {
  Bind("A._x", "a");

  // {
  //   "L" : [
  //     {
  //       "A" : {
  //         "x" : "b"
  //       },
  //     },
  //   ],
  //   "A" : ["c", "d"]
  //   "A" : {
  //     "y" : "e",
  //     ("_x" : "a"),
  //   },
  // }

  expect_.StartObject("");
  expect_.StartList("L");
  expect_.StartObject("");
  expect_.StartObject("A");
  expect_.RenderString("x", "b");
  expect_.EndObject();  // "A"
  expect_.EndObject();  // ""
  expect_.EndList();    // "L"
  expect_.StartList("A");
  expect_.RenderString("", "c");
  expect_.RenderString("", "d");
  expect_.EndList();  // "A"
  expect_.StartObject("A");
  expect_.RenderString("y", "e");
  expect_.RenderString("_x", "a");
  expect_.EndObject();  // "A"
  expect_.EndObject();  // ""

  auto w = Create();

  w->StartObject("");
  w->StartList("L");
  w->StartObject("");
  w->StartObject("A");
  w->RenderString("x", "b");
  w->EndObject();  // "A"
  w->EndObject();  // ""
  w->EndList();    // "L"
  w->StartList("A");
  w->RenderString("", "c");
  w->RenderString("", "d");
  w->EndList();  // "A"
  w->StartObject("A");
  w->RenderString("y", "e");
  w->EndObject();  // "A"
  w->EndObject();  // ""
}

}  // namespace
}  // namespace testing
}  // namespace transcoding

}  // namespace api_manager
}  // namespace google
