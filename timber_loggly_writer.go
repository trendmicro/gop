package gop

import (
	"fmt"
	"os"

	"github.com/cocoonlife/go-loggly"
)

// A timber.LogWriter for the loggly service.

// LogglyWriter is a Timber writer to send logging to the loggly
// service. See: https://loggly.com.
type LogglyWriter struct {
	c *loggly.Client
}

// NewLogEntriesWriter creates a new writer for sending logging to Loggly.
func NewLogglyWriter(token string, tags ...string) (*LogglyWriter, error) {
	return &LogglyWriter{c: loggly.New(token, tags...)}, nil
}

// LogWrite the message to the logenttries server async. Satifies the timber.LogWrite interface.
func (w *LogglyWriter) LogWrite(msg string) {
	// using type for the message string is how the Info etc methods on the
	// loggly client work.
	// TODO: Add a "level" key for info, error..., proper timestamp etc
	// Buffers the message for async send
	// TODO - Stat for the bytes written return?
	if _, err := w.c.Write([]byte(msg)); err != nil {
		// TODO: What is best todo here as if we log it will loop?
		fmt.Fprintf(os.Stderr, "loggly send error: %s\n", err.Error())
	}
}

// Close the write. Satifies the timber.LogWriter interface.
func (w *LogglyWriter) Close() {
	w.c.Flush()
	close(w.c.ShutdownChan)
}
