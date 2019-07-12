package utils

import (
	"os"
	"strings"
)

func FletcherChecksum8(inp string) uint8 {
	var ckA, ckB uint8
	for i := 0; i < len(inp); i++ {
		ckA = (ckA + inp[i]) & 0xf
		ckB = (ckB + ckA) & 0xf
	}
	return (ckB << 4) | ckA
}

func ShortHostname() (shortName string, err error) {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	splitHostname := strings.SplitN(hostname, ".", 2)
	shortName = splitHostname[0]
	return shortName, err
}
