package ledgerforge

import (
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

var testRedisServer *miniredis.Miniredis

func TestMain(m *testing.M) {
	var err error
	testRedisServer, err = miniredis.Run()
	if err != nil {
		panic(err)
	}

	code := m.Run()

	testRedisServer.Close()
	os.Exit(code)
}

func testRedisAddr() string {
	if testRedisServer == nil {
		panic("test redis server is not initialized")
	}
	return testRedisServer.Addr()
}
