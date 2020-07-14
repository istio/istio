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

package stdio // import "istio.io/istio/mixer/adapter/stdio"

import (
	"os"
	"time"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"istio.io/istio/mixer/adapter/stdio/config"
)

var levelToZap = map[config.Params_Level]zapcore.Level{
	config.INFO:    zapcore.InfoLevel,
	config.WARNING: zapcore.WarnLevel,
	config.ERROR:   zapcore.ErrorLevel,
}

// Creates a zap core based on the given options
func newZapCore(options *config.Params) (zapcore.Core, func(), error) {
	encCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "instance",
		CallerKey:      "caller",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeTime:     formatDate,
	}

	var enc zapcore.Encoder
	if options.OutputAsJson {
		enc = zapcore.NewJSONEncoder(encCfg)
	} else {
		enc = zapcore.NewConsoleEncoder(encCfg)
	}

	var outputSink zapcore.WriteSyncer
	var err error
	var closer = func() {}

	switch options.LogStream {
	case config.ROTATED_FILE:
		lj := &lumberjack.Logger{
			Filename:   options.OutputPath,
			MaxSize:    int(options.MaxMegabytesBeforeRotation),
			MaxBackups: int(options.MaxDaysBeforeRotation),
			MaxAge:     int(options.MaxRotatedFiles),
		}

		outputSink = zapcore.AddSync(lj)
		closer = func() { _ = lj.Close() }

	case config.FILE:
		if outputSink, closer, err = zap.Open(options.OutputPath); err != nil {
			return nil, nil, err
		}

	case config.STDOUT:
		outputSink = os.Stdout

	case config.STDERR:
		outputSink = os.Stderr
	}

	return zapcore.NewCore(enc, outputSink, zap.NewAtomicLevelAt(levelToZap[options.OutputLevel])), closer, nil
}

func formatDate(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	t = t.UTC()
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	micros := t.Nanosecond() / 1000

	buf := make([]byte, 27)

	buf[0] = byte((year/1000)%10) + '0'
	buf[1] = byte((year/100)%10) + '0'
	buf[2] = byte((year/10)%10) + '0'
	buf[3] = byte(year%10) + '0'
	buf[4] = '-'
	buf[5] = byte((month)/10) + '0'
	buf[6] = byte((month)%10) + '0'
	buf[7] = '-'
	buf[8] = byte((day)/10) + '0'
	buf[9] = byte((day)%10) + '0'
	buf[10] = 'T'
	buf[11] = byte((hour)/10) + '0'
	buf[12] = byte((hour)%10) + '0'
	buf[13] = ':'
	buf[14] = byte((minute)/10) + '0'
	buf[15] = byte((minute)%10) + '0'
	buf[16] = ':'
	buf[17] = byte((second)/10) + '0'
	buf[18] = byte((second)%10) + '0'
	buf[19] = '.'
	buf[20] = byte((micros/100000)%10) + '0'
	buf[21] = byte((micros/10000)%10) + '0'
	buf[22] = byte((micros/1000)%10) + '0'
	buf[23] = byte((micros/100)%10) + '0'
	buf[24] = byte((micros/10)%10) + '0'
	buf[25] = byte((micros)%10) + '0'
	buf[26] = 'Z'

	enc.AppendString(string(buf))
}
