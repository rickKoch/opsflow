package logging

import "log"

// Logger is a minimal structured logger interface.
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
}

// StdLogger is a simple stdlib logger implementation that satisfies Logger.
type StdLogger struct{}

func (StdLogger) Info(msg string, keysAndValues ...interface{}) {
	log.Printf("INFO: %s %v", msg, keysAndValues)
}
func (StdLogger) Warn(msg string, keysAndValues ...interface{}) {
	log.Printf("WARN: %s %v", msg, keysAndValues)
}
func (StdLogger) Error(msg string, keysAndValues ...interface{}) {
	log.Printf("ERROR: %s %v", msg, keysAndValues)
}
func (StdLogger) Debug(msg string, keysAndValues ...interface{}) {
	log.Printf("DEBUG: %s %v", msg, keysAndValues)
}
