package gop

// A timer.LogWriter for the logentries service.

import "github.com/bsphere/le_go"

// LogEntriesWriter is a Timebr writer to send logging to the logentries
// service. See: https://logentries.com.
type LogEntriesWriter struct {
	le *le_go.Logger
}

// NewLogEntriesWriter creates a new writer for sending logging to logentries.
func NewLogEntriesWriter(token string) (*LogEntriesWriter, error) {
	if le, err := le_go.Connect(token); err != nil {
		return nil, err
	} else {
		return &LogEntriesWriter{le: le}, nil
	}
}

// LogWrite the message to the logenttries server async. Satifies the timber.LogWrite interface.
func (w *LogEntriesWriter) LogWrite(msg string) {
	// XXX Quick and dirty async. Really need some kind of buffer and drop
	// policy for slow, broken network writing.
	go w.le.Println(msg)
}

// Close the write. Satifies the timber.LogWriter interface.
func (w *LogEntriesWriter) Close() {
	w.le.Close()
}
