//go:build load

package load

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	started := time.Now()
	code := m.Run()
	time.Sleep(10 * time.Second)
	if err := snapshot(profileName(), started, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "dashboard snapshot:", err)
		code = 1
	}
	fmt.Println("load reports:", reportDir())
	os.Exit(code)
}
