package wa

import (
	"fmt"
	"io"
	"log"

	waLog "github.com/polymorfa/hypermeow/util/log"
)

const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
)

// fileLogger adapts hypermeow's logger interface to a log.Logger. The GUI
// build has no console, so logs go to a file in the data directory.
type fileLogger struct {
	mod string
	out *log.Logger
	min int
}

func newLogger(w io.Writer, debug bool) waLog.Logger {
	min := levelInfo
	if debug {
		min = levelDebug
	}
	return &fileLogger{out: log.New(w, "", log.LstdFlags|log.Lmicroseconds), min: min}
}

func (l *fileLogger) logf(level int, tag, msg string, args ...any) {
	if level < l.min {
		return
	}
	l.out.Printf("[%s %s] %s", l.mod, tag, fmt.Sprintf(msg, args...))
}

func (l *fileLogger) Debugf(msg string, args ...any) { l.logf(levelDebug, "DEBUG", msg, args...) }
func (l *fileLogger) Infof(msg string, args ...any)  { l.logf(levelInfo, "INFO", msg, args...) }
func (l *fileLogger) Warnf(msg string, args ...any)  { l.logf(levelWarn, "WARN", msg, args...) }
func (l *fileLogger) Errorf(msg string, args ...any) { l.logf(levelError, "ERROR", msg, args...) }

func (l *fileLogger) Sub(module string) waLog.Logger {
	mod := module
	if l.mod != "" {
		mod = l.mod + "/" + module
	}
	return &fileLogger{mod: mod, out: l.out, min: l.min}
}
