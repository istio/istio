// Copyright 2017 Istio Authors
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

// Package log provides the canonical logging functionality used by Go-based
// Istio components.
//
// Istio's logging subsystem is built on top of the [Zap](https://godoc.org/go.uber.org/zap) package.
// High performance scenarios should use the Error, Warn, Info, and Debug methods. Lower perf
// scenarios can use the more expensive convenience methods such as Debugf and Warnw.
//
// The package provides direct integration with the Cobra command-line processor which makes it
// easy to build programs that use a consistent interface for logging. Here's an example
// of a simple Cobra-based program using this log package:
//
//		func main() {
//			// get the default logging options
//			options := log.NewOptions()
//
//			rootCmd := &cobra.Command{
//				Run: func(cmd *cobra.Command, args []string) {
//
//					// configure the logging system
//					if err := log.Configure(options); err != nil {
//                      // print an error and quit
//                  }
//
//					// output some logs
//					log.Info("Hello")
//					log.Sync()
//				},
//			}
//
//			// add logging-specific flags to the cobra command
//			options.AttachCobraFlags(rootCmd)
//			rootCmd.SetArgs(os.Args[1:])
//			rootCmd.Execute()
//		}
//
// Once configured, this package intercepts the output of the standard golang "log" package as well as anything
// sent to the global zap logger (zap.L()).
package log

import (
	"github.com/natefinch/lumberjack"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zapgrpc"
	"google.golang.org/grpc/grpclog"
)

// Configure initializes Istio's logging subsystem.
//
// You typically call this once at process startup.
// Once this call returns, the logging system is ready to accept data.
func Configure(options *Options) error {
	outputLevel, err := options.GetOutputLevel()
	if err != nil {
		// bad format specified
		return err
	}

	stackTraceLevel, err := options.GetStackTraceLevel()
	if err != nil {
		// bad format specified
		return err
	}

	if outputLevel == None || ((len(options.OutputPaths) == 0) && options.RotateOutputPath == "") {
		// stick with the Nop default
		logger = zap.NewNop()
		sugar = logger.Sugar()
		return nil
	}

	encCfg := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeTime:     formatDate,
	}

	var enc zapcore.Encoder
	if options.JSONEncoding {
		enc = zapcore.NewJSONEncoder(encCfg)
	} else {
		enc = zapcore.NewConsoleEncoder(encCfg)
	}

	var rotaterSink zapcore.WriteSyncer
	if options.RotateOutputPath != "" {
		rotaterSink = zapcore.AddSync(&lumberjack.Logger{
			Filename:   options.RotateOutputPath,
			MaxSize:    options.RotationMaxSize,
			MaxBackups: options.RotationMaxAge,
			MaxAge:     options.RotationMaxBackups,
		})
	}

	errSink, closeErrorSink, err := zap.Open(options.ErrorOutputPaths...)
	if err != nil {
		return err
	}

	var outputSink zapcore.WriteSyncer
	if len(options.OutputPaths) > 0 {
		outputSink, _, err = zap.Open(options.OutputPaths...)
		if err != nil {
			closeErrorSink()
			return err
		}
	}

	var sink zapcore.WriteSyncer
	if rotaterSink != nil && outputSink != nil {
		sink = zapcore.NewMultiWriteSyncer(outputSink, rotaterSink)
	} else if rotaterSink != nil {
		sink = rotaterSink
	} else {
		sink = outputSink
	}

	opts := []zap.Option{zap.ErrorOutput(errSink)}

	if options.IncludeCallerSourceLocation {
		opts = append(opts, zap.AddCaller())
	}

	if stackTraceLevel != None {
		opts = append(opts, zap.AddStacktrace(stackTraceLevel))
	}

	l := zap.New(
		zapcore.NewCore(enc, sink, zap.NewAtomicLevelAt(outputLevel)),
		opts...,
	)

	logger = l.WithOptions(zap.AddCallerSkip(1), zap.AddStacktrace(stackTraceLevel))
	sugar = logger.Sugar()

	// capture global zap logging and force it through our logger
	_ = zap.ReplaceGlobals(l)

	// capture standard golang "log" package output and force it through our logger
	_ = zap.RedirectStdLog(logger)

	// capture gRPC logging
	grpclog.SetLogger(zapgrpc.NewLogger(logger.WithOptions(zap.AddCallerSkip(2))))

	return nil
}
