package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Default size limits
const (
	DefaultMaxEventSize = 1024 * 1024      // 1MB default max event size
	MaxAllowedEventSize = 10 * 1024 * 1024 // 10MB absolute maximum
)

// Errors
var (
	ErrEventTooLarge = errors.New("event data exceeds maximum allowed size")
	ErrWriterClosed  = errors.New("writer is closed")
)

// Event represents a Server-Sent Event
type Event struct {
	ID      string      // Event ID
	Event   string      // Event type (e.g., "message", "error")
	Data    interface{} // Event data (will be JSON encoded if not string)
	Retry   int         // Retry interval in milliseconds
	RawData []byte      // Raw data bytes
}

// Writer writes SSE events to an HTTP response
type Writer struct {
	w            http.ResponseWriter
	flusher      http.Flusher
	mu           sync.Mutex
	closed       bool
	keepalive    time.Duration
	stopCh       chan struct{}
	lastEventID  string
	maxEventSize int // Maximum event size in bytes
	ctx          context.Context // Request context for cleanup
}

// WriterOption configures a Writer
type WriterOption func(*Writer)

// WithKeepalive sets the keepalive interval
func WithKeepalive(d time.Duration) WriterOption {
	return func(w *Writer) {
		w.keepalive = d
	}
}

// WithMaxEventSize sets the maximum event size
func WithMaxEventSize(size int) WriterOption {
	return func(w *Writer) {
		if size > 0 && size <= MaxAllowedEventSize {
			w.maxEventSize = size
		}
	}
}

// WithContext sets the request context for cleanup
func WithContext(ctx context.Context) WriterOption {
	return func(w *Writer) {
		w.ctx = ctx
	}
}

// NewWriter creates a new SSE writer
func NewWriter(w http.ResponseWriter, opts ...WriterOption) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	writer := &Writer{
		w:            w,
		flusher:      flusher,
		stopCh:       make(chan struct{}),
		maxEventSize: DefaultMaxEventSize,
		ctx:          context.Background(), // Default to background context
	}

	for _, opt := range opts {
		opt(writer)
	}

	// Start keepalive if configured
	if writer.keepalive > 0 {
		go writer.keepaliveLoop()
	}

	return writer, nil
}

// WriteEvent writes an SSE event
func (w *Writer) WriteEvent(event *Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return ErrWriterClosed
	}

	// Check event size before processing
	eventSize := len(event.RawData)
	if event.Data != nil {
		switch v := event.Data.(type) {
		case string:
			eventSize = len(v)
		case []byte:
			eventSize = len(v)
		}
	}
	if eventSize > w.maxEventSize {
		return ErrEventTooLarge
	}

	var sb strings.Builder

	// Write event ID
	if event.ID != "" {
		sb.WriteString("id: ")
		sb.WriteString(event.ID)
		sb.WriteString("\n")
		w.lastEventID = event.ID
	}

	// Write event type
	if event.Event != "" {
		sb.WriteString("event: ")
		sb.WriteString(event.Event)
		sb.WriteString("\n")
	}

	// Write retry
	if event.Retry > 0 {
		sb.WriteString(fmt.Sprintf("retry: %d\n", event.Retry))
	}

	// Write data
	var data string
	if event.RawData != nil {
		data = string(event.RawData)
	} else if event.Data != nil {
		switch v := event.Data.(type) {
		case string:
			data = v
		case []byte:
			data = string(v)
		default:
			jsonData, err := json.Marshal(event.Data)
			if err != nil {
				return fmt.Errorf("failed to marshal event data: %w", err)
			}
			data = string(jsonData)
		}
	}

	// Write each line with "data: " prefix
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		sb.WriteString("data: ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	// End event with blank line
	sb.WriteString("\n")

	// Write to response
	_, err := io.WriteString(w.w, sb.String())
	if err != nil {
		return err
	}

	w.flusher.Flush()
	return nil
}

// WriteData writes a data-only event
func (w *Writer) WriteData(data interface{}) error {
	return w.WriteEvent(&Event{Data: data})
}

// WriteJSON writes JSON data as an event
func (w *Writer) WriteJSON(data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return w.WriteEvent(&Event{RawData: jsonData})
}

// WriteDone writes the [DONE] terminator (OpenAI compatible)
func (w *Writer) WriteDone() error {
	return w.WriteEvent(&Event{Data: "[DONE]"})
}

// WriteError writes an error event
func (w *Writer) WriteError(err error) error {
	return w.WriteEvent(&Event{
		Event: "error",
		Data:  map[string]string{"error": err.Error()},
	})
}

// WriteComment writes an SSE comment (used for keepalive)
func (w *Writer) WriteComment(comment string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return fmt.Errorf("writer is closed")
	}

	_, err := io.WriteString(w.w, ": "+comment+"\n\n")
	if err != nil {
		return err
	}

	w.flusher.Flush()
	return nil
}

// Close closes the SSE writer
func (w *Writer) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.closed {
		w.closed = true
		close(w.stopCh)
	}
}

// IsClosed returns true if the writer is closed
func (w *Writer) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// keepaliveLoop sends periodic keepalive comments
// Exits when Close() is called, context is cancelled, or write fails
func (w *Writer) keepaliveLoop() {
	ticker := time.NewTicker(w.keepalive)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := w.WriteComment("keepalive"); err != nil {
				return
			}
		case <-w.stopCh:
			return
		case <-w.ctx.Done():
			// Context cancelled (client disconnected, request timeout, etc.)
			return
		}
	}
}

// LastEventID returns the last event ID sent
func (w *Writer) LastEventID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastEventID
}

// Reader reads SSE events from an io.Reader
type Reader struct {
	reader       *bufio.Reader
	lastID       string
	eventCh      chan Event
	errCh        chan error
	maxEventSize int // Maximum event data size in bytes
}

// ReaderOption configures a Reader
type ReaderOption func(*Reader)

// WithReaderMaxEventSize sets the maximum event size for the reader
func WithReaderMaxEventSize(size int) ReaderOption {
	return func(r *Reader) {
		if size > 0 && size <= MaxAllowedEventSize {
			r.maxEventSize = size
		}
	}
}

// NewReader creates a new SSE reader
func NewReader(r io.Reader, opts ...ReaderOption) *Reader {
	reader := &Reader{
		reader:       bufio.NewReader(r),
		eventCh:      make(chan Event, 10),
		errCh:        make(chan error, 1),
		maxEventSize: DefaultMaxEventSize,
	}
	for _, opt := range opts {
		opt(reader)
	}
	return reader
}

// Read reads SSE events in a loop
func (r *Reader) Read(ctx context.Context) (<-chan Event, <-chan error) {
	go r.readLoop(ctx)
	return r.eventCh, r.errCh
}

// lineResult represents the result of reading a line
type lineResult struct {
	line string
	err  error
}

// readLoop reads SSE events until EOF or context cancellation.
// Uses a separate goroutine for reading to properly handle context cancellation
// even when the underlying read is blocked.
func (r *Reader) readLoop(ctx context.Context) {
	defer close(r.eventCh)
	defer close(r.errCh)

	// Create a channel for line results
	lineCh := make(chan lineResult, 1)

	// Start a goroutine to read lines
	// This goroutine will exit when the reader returns an error (including EOF)
	// or when the connection is closed
	go func() {
		defer close(lineCh)
		for {
			line, err := r.reader.ReadString('\n')
			select {
			case lineCh <- lineResult{line: line, err: err}:
				if err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	var event Event

	for {
		select {
		case <-ctx.Done():
			r.errCh <- ctx.Err()
			return
		case result, ok := <-lineCh:
			if !ok {
				// Channel closed, reader goroutine has exited
				return
			}

			if result.err != nil {
				if result.err != io.EOF {
					r.errCh <- result.err
				}
				return
			}

			line := strings.TrimRight(result.line, "\r\n")

			// Empty line = dispatch event
			if line == "" {
				if event.Event != "" || event.Data != nil || len(event.RawData) > 0 {
					event.ID = r.lastID
					select {
					case r.eventCh <- event:
					case <-ctx.Done():
						return
					}
					event = Event{}
				}
				continue
			}

			// Comment line
			if strings.HasPrefix(line, ":") {
				continue
			}

			// Parse field
			colonIdx := strings.Index(line, ":")
			var field, value string
			if colonIdx == -1 {
				field = line
				value = ""
			} else {
				field = line[:colonIdx]
				value = line[colonIdx+1:]
				if len(value) > 0 && value[0] == ' ' {
					value = value[1:]
				}
			}

			switch field {
			case "event":
				event.Event = value
			case "data":
				// Check size limit before appending data
				newSize := len(event.RawData) + len(value) + 1 // +1 for newline
				if newSize > r.maxEventSize {
					r.errCh <- ErrEventTooLarge
					return
				}
				if event.RawData == nil {
					event.RawData = []byte(value)
				} else {
					event.RawData = append(event.RawData, '\n')
					event.RawData = append(event.RawData, []byte(value)...)
				}
			case "id":
				r.lastID = value
			case "retry":
				// Parse retry value (ignored for now)
			}
		}
	}
}
