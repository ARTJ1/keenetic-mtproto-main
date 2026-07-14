//go:build !linux

package mtproto

import (
	"net"
	"time"
)

const defaultUserTimeout = 120 * time.Second

func setTCPUserTimeout(c net.Conn, d time.Duration) {}
