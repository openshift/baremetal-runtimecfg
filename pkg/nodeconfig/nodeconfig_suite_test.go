package nodeconfig

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestNodeConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NodeConfig Suite")
}

var _ = BeforeSuite(func() {
	// Test suite setup if needed
})

var _ = AfterSuite(func() {
	// Test suite cleanup if needed
})
