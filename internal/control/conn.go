package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// QueueSize is how many lines wait to be written before sends fail.
const QueueSize = 256

// ErrTooLong: a message does not fit in one line.
var ErrTooLong = errors.New("message longer than a line may be")

// Conn writes lines to the other side. Sends never block: they go to a
// queue that one goroutine writes out.
type Conn struct {
	w        io.Writer
	queue    chan []byte
	pending  atomic.Int64 // queued, not written yet
	done     chan struct{}
	stopOnce sync.Once
	overflow func()
	overOnce sync.Once
}

// NewConn starts writing to w. overflow is called once when a message
// that may not be dropped finds the queue full: the other side stopped
// reading.
func NewConn(w io.Writer, overflow func()) *Conn {
	c := &Conn{w: w, queue: make(chan []byte, QueueSize), done: make(chan struct{}), overflow: overflow}
	go c.write()
	return c
}

func (c *Conn) write() {
	for {
		select {
		case <-c.done:
			return
		case b := <-c.queue:
			_, err := c.w.Write(b)
			c.pending.Add(-1)
			if err != nil {
				c.Close()
				return
			}
		}
	}
}

// Send queues v as one line and reports whether it was queued.
// droppable: a status, which the next one replaces, is dropped when the
// queue is full. Anything else that does not fit calls overflow.
func (c *Conn) Send(v any, droppable bool) bool {
	b, err := json.Marshal(v)
	if err != nil || len(b)+1 >= MaxLine {
		return false
	}
	b = append(b, '\n')
	select {
	case <-c.done:
		return false
	default:
	}
	c.pending.Add(1)
	select {
	case c.queue <- b:
		return true
	default:
		c.pending.Add(-1)
	}
	if !droppable && c.overflow != nil {
		c.overOnce.Do(c.overflow)
	}
	return false
}

// Flush waits until the queue is written, at most for d.
func (c *Conn) Flush(d time.Duration) bool {
	end := time.Now().Add(d)
	for c.pending.Load() > 0 {
		select {
		case <-c.done:
			return false
		default:
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

// Close stops the writer; what is still queued is dropped.
func (c *Conn) Close() { c.stopOnce.Do(func() { close(c.done) }) }

// Done is closed when the writer stops.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Read calls f with every line from r until r ends. A line that is too
// long or not a message ends it with an error.
func Read(r io.Reader, f func(Line)) error {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 0, 64<<10), MaxLine)
	for s.Scan() {
		b := s.Bytes()
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		l, err := Parse(b)
		if err != nil {
			return fmt.Errorf("bad line: %w", err)
		}
		f(l)
	}
	return s.Err()
}

// Client sends requests over a Conn and matches the replies to them.
type Client struct {
	conn    *Conn
	mu      sync.Mutex
	next    int64
	pending map[int64]chan Line
	closed  bool
}

func NewClient(conn *Conn) *Client {
	return &Client{conn: conn, pending: map[int64]chan Line{}}
}

// Call sends m with params p and waits for the reply's result.
func (c *Client) Call(ctx context.Context, m string, p any) (json.RawMessage, error) {
	var raw json.RawMessage
	if p != nil {
		b, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	ch := make(chan Line, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, &Error{Code: CodeGone, Msg: "EDSense is not there"}
	}
	c.next++
	id := c.next
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if !c.conn.Send(Request{ID: id, M: m, P: raw}, false) {
		return nil, &Error{Code: CodeGone, Msg: "EDSense is not reading"}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case l := <-ch:
		if l.Err != nil {
			return nil, l.Err
		}
		return l.OK, nil
	}
}

// Deliver hands a reply to its call. It reports false for a line that is
// no reply.
func (c *Client) Deliver(l Line) bool {
	if l.M != "" || l.Ev != "" {
		return false
	}
	c.mu.Lock()
	ch, ok := c.pending[l.ID]
	c.mu.Unlock()
	if ok {
		select {
		case ch <- l:
		default:
		}
	}
	return true
}

// Close fails the calls waiting and every later one.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for id, ch := range c.pending {
		select {
		case ch <- Line{ID: id, Err: &Error{Code: CodeGone, Msg: "EDSense went away"}}:
		default:
		}
	}
}
