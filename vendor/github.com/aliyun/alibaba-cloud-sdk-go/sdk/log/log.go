package log

import (
	"github.com/sirupsen/logrus"
	"os"
)

func init() {
	logrus.SetOutput(os.Stdout)
	logrus.SetFormatter(&logrus.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})

}

func WithFields(fields logrus.Fields) *logrus.Entry {
	return logrus.WithFields(logrus.Fields(fields))
}

var (
	PanicLevel = logrus.PanicLevel
	FatalLevel = logrus.FatalLevel
	ErrorLevel = logrus.ErrorLevel
	WarnLevel = logrus.WarnLevel
	InfoLevel = logrus.InfoLevel
	DebugLevel = logrus.DebugLevel

	SetLevel = logrus.SetLevel
	GetLevel = logrus.GetLevel

	WithError = logrus.WithError
	WithField = logrus.WithField

	Debug   = logrus.Debug
	Print   = logrus.Print
	Info    = logrus.Info
	Warn    = logrus.Warn
	Warning = logrus.Warning
	Error   = logrus.Error
	Panic   = logrus.Panic
	Fatal   = logrus.Fatal

	Debugf   = logrus.Debugf
	Printf   = logrus.Printf
	Infof    = logrus.Infof
	Warnf    = logrus.Warnf
	Warningf = logrus.Warningf
	Errorf   = logrus.Errorf
	Panicf   = logrus.Panicf
	Fatalf   = logrus.Fatalf
)
