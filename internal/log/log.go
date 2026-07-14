package log

import (
	"fmt"
	"log"
	"os"
	"sync/atomic"
)

type Level int32

const (
	LevelError Level = iota
	LevelInfo
	LevelTrace
	LevelDebug
)

var curLevel atomic.Int32

func init() {
	curLevel.Store(int32(LevelInfo))
}

func SetLevel(l Level) { curLevel.Store(int32(l)) }

func out(prefix, format string, a ...any) {
	log.Output(3, prefix+fmt.Sprintf(format, a...))
}

func Errorf(format string, a ...any) error {
	msg := fmt.Sprintf(format, a...)
	_ = log.Output(2, "[ERROR] "+msg)
	return fmt.Errorf("%s", msg)
}

func Warnf(format string, a ...any) {
	if Level(curLevel.Load()) >= LevelError {
		out("[WARN] ", format, a...)
	}
}

func Infof(format string, a ...any) {
	if Level(curLevel.Load()) >= LevelInfo {
		out("[INFO] ", format, a...)
	}
}

func Tracef(format string, a ...any) {
	if Level(curLevel.Load()) >= LevelTrace {
		out("[TRACE] ", format, a...)
	}
}

func Debugf(format string, a ...any) {
	if Level(curLevel.Load()) >= LevelDebug {
		out("[DEBUG] ", format, a...)
	}
}

func Init() {
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
}

// LogConnectionStr mirrors B4 connection logging; kept as a lightweight info line.
func LogConnectionStr(protocol, sniSet, domain, source, ipSet, destination, srcMac, tlsVersion, metadata string) {
	Infof("conn proto=%s domain=%s src=%s dst=%s meta=%s", protocol, domain, source, destination, metadata)
}
