// Package control is the protocol between EDSense's core (the tray
// process) and its window process: newline-delimited JSON over the
// window's stdin and stdout. The allowlist of what the window may ask is
// kept here, in one table, checked in the window and enforced in the core.
package control

import (
	"encoding/json"
	"errors"
)

// Proto is the protocol's version. Both sides are the same exe, so a
// mismatch means the exe was replaced while the core ran.
const Proto = 1

// MaxLine is the longest line either side reads.
const MaxLine = 1 << 20

// Request is a call from the window: {"id":7,"m":"pause.set","p":{"paused":true}}.
type Request struct {
	ID int64           `json:"id"`
	M  string          `json:"m"`
	P  json.RawMessage `json:"p,omitempty"`
}

// Reply answers a request: {"id":7,"ok":{...}} or {"id":7,"err":{...}}.
type Reply struct {
	ID  int64  `json:"id"`
	OK  any    `json:"ok,omitempty"`
	Err *Error `json:"err,omitempty"`
}

// Event is sent without being asked for: {"ev":"status","d":{...}}.
type Event struct {
	Ev string `json:"ev"`
	D  any    `json:"d,omitempty"`
}

// Error is a refused or failed request.
type Error struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

// Error codes.
const (
	CodeUnknown = "unknown" // not in the allowlist
	CodeParams  = "params"  // bad parameters
	CodeOrder   = "order"   // hello must come first
	CodeProto   = "proto"   // the two sides speak different versions
	CodeFailed  = "failed"  // the core could not do it
	CodeGone    = "gone"    // the other side went away
)

// Line is any message, as read: a request has M, an event Ev, a reply
// neither.
type Line struct {
	ID  int64           `json:"id"`
	M   string          `json:"m"`
	P   json.RawMessage `json:"p"`
	OK  json.RawMessage `json:"ok"`
	Err *Error          `json:"err"`
	Ev  string          `json:"ev"`
	D   json.RawMessage `json:"d"`
}

// Parse reads one line.
func Parse(b []byte) (Line, error) {
	var l Line
	if err := json.Unmarshal(b, &l); err != nil {
		return Line{}, err
	}
	if l.M != "" && l.Ev != "" {
		return Line{}, errors.New("a line is a request or an event, not both")
	}
	return l, nil
}
