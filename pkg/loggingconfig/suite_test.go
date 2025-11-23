package loggingconfig

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestLoggingConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LoggingConfig Suite")
}
