package internal

import (
	"fmt"
	"log"
	"os"
)

// ContextLogger extends the standard logger with contextual information
type ContextLogger struct {
	*log.Logger
	context string
}

// NewContextLogger creates a new logger with the given context prefix
func NewContextLogger(context string) *ContextLogger {
	return &ContextLogger{
		Logger:  log.New(os.Stdout, "", log.LstdFlags),
		context: context,
	}
}

// Printf logs a formatted message with context
func (l *ContextLogger) Printf(format string, v ...interface{}) {
	if l.context != "" {
		format = fmt.Sprintf("[%s] %s", l.context, format)
	}
	l.Logger.Printf(format, v...)
}

// WithContext creates a new logger with additional context
func (l *ContextLogger) WithContext(context string) *ContextLogger {
	var newContext string
	if l.context == "" {
		newContext = context
	} else {
		newContext = fmt.Sprintf("%s | %s", l.context, context)
	}
	return NewContextLogger(newContext)
}
