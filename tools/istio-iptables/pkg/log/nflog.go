// Copyright Istio Authors
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

package log

import (
	"context"
	golog "log"
	"net"
	"os"

	"github.com/florianl/go-nflog/v2"
	"golang.org/x/net/ipv4"

	"istio.io/pkg/env"
	"istio.io/pkg/log"
)

var TraceLoggingEnabled = env.Register(
	"IPTABLES_TRACE_LOGGING",
	false,
	"When enable, all iptables actions will be logged. "+
		"This requires NET_ADMIN privilege and has noisy logs; as a result, this is intended for debugging only").Get()

var iptablesTrace = log.RegisterScope("iptables", "trace logs for iptables", 0)

// ReadNFLOGSocket reads from the nflog socket, sending output to logs.
// This is intended for debugging only.
func ReadNFLOGSocket(ctx context.Context) {
	if !TraceLoggingEnabled {
		return
	}
	iptablesTrace.Infof("starting nftable log")
	// We expect to read logs from rules like:
	// `-j NFLOG --nflog-group 1337`
	config := nflog.Config{
		Group:    1337,
		Copymode: nflog.CopyPacket,
		Logger:   golog.New(os.Stdout, "nflog log", 0),
	}

	nf, err := nflog.Open(&config)
	if err != nil {
		log.Errorf("could not open nflog socket: %v", err)
		return
	}
	defer nf.Close()

	fn := func(attrs nflog.Attribute) int {
		src, dst := "", ""
		if attrs.Payload != nil {
			v4, err := ipv4.ParseHeader(*attrs.Payload)
			if err == nil {
				src = v4.Src.String()
				dst = v4.Dst.String()
			}
		}
		prefix := ""
		if attrs.Prefix != nil {
			prefix = *attrs.Prefix
		}
		comment := IDToCommand[prefix].Comment
		uid, gid := uint32(0), uint32(0)
		if attrs.UID != nil {
			uid = *attrs.UID
		}
		if attrs.GID != nil {
			gid = *attrs.GID
		}
		inDev, outDev := "", ""
		if attrs.InDev != nil {
			ii, err := net.InterfaceByIndex(int(*attrs.InDev))
			if err == nil {
				inDev = ii.Name
			}
		}
		if attrs.OutDev != nil {
			ii, err := net.InterfaceByIndex(int(*attrs.OutDev))
			if err == nil {
				outDev = ii.Name
			}
		}
		iptablesTrace.WithLabels(
			"command", prefix,
			"uid", uid,
			"gid", gid,
			"inDev", inDev,
			"outDev", outDev,
			"src", src,
			"dst", dst,
		).Infof(comment)
		return 0
	}

	// Register our callback for the nflog
	err = nf.RegisterWithErrorFunc(ctx, fn, func(e error) int {
		iptablesTrace.Warnf("log failed: %v", e)
		return 0
	})
	if err != nil {
		log.Errorf("failed to register nflog callback: %v", err)
		return
	}

	// Block util the context expires
	<-ctx.Done()
}
