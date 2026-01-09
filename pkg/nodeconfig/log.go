package nodeconfig

import (
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

func init() {
	log.SetLevel(logrus.InfoLevel)
}

// SetDebugLogLevel sets the log level to debug
func SetDebugLogLevel() {
	log.SetLevel(logrus.DebugLevel)
}

// SetInfoLogLevel sets the log level to info
func SetInfoLogLevel() {
	log.SetLevel(logrus.InfoLevel)
}
