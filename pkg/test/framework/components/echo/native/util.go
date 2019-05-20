// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package native

import (
	"crypto/rand"
	"encoding/base64"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pkg/test/util/reserveport"
)

const (
	localhost = "127.0.0.1"
)

func findFreePort(portMgr reserveport.PortManager) (int, error) {
	reservedPort, err := portMgr.ReservePort()
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = reservedPort.Close()
	}()

	return int(reservedPort.GetPort()), nil
}

func randomBase64String(length int) string {
	buff := make([]byte, length)
	_, _ = rand.Read(buff)
	str := base64.URLEncoding.EncodeToString(buff)
	return str[:length]
}

func createTempFile(tmpDir, prefix, suffix string) (string, error) {
	f, err := ioutil.TempFile(tmpDir, prefix)
	if err != nil {
		return "", err
	}
	var tmpName string
	if tmpName, err = filepath.Abs(f.Name()); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Remove(tmpName); err != nil {
		return "", err
	}
	return tmpName + suffix, nil
}

func pb2Json(pb proto.Message) string {
	m := jsonpb.Marshaler{
		Indent: "  ",
	}
	str, _ := m.MarshalToString(pb)
	return str
}
