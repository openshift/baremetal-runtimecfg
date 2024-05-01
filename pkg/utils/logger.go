package utils

import "github.com/sirupsen/logrus"

var log = logrus.New()

func SetDebugLogLevel() {
	log.SetLevel(logrus.DebugLevel)
}

func SetInfoLogLevel() {
	log.SetLevel(logrus.InfoLevel)
}
