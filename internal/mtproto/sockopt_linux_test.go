//go:build linux

package mtproto

import (
	"net"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSetTCPUserTimeoutAppliesToSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	setTCPUserTimeout(c, 45*time.Second)

	raw, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var got int
	var gerr error
	if cerr := raw.Control(func(fd uintptr) {
		got, gerr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT)
	}); cerr != nil {
		t.Fatal(cerr)
	}
	if gerr != nil {
		t.Fatal(gerr)
	}
	if got != 45000 {
		t.Fatalf("TCP_USER_TIMEOUT = %d ms, want 45000", got)
	}
}
