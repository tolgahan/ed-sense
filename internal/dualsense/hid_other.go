//go:build !windows

package dualsense

import "time"

// The controller is only reached on Windows.

func ListHID() []HIDDevice { return nil }

type Link struct{}

func NewLink() *Link                              { return &Link{} }
func (l *Link) Available() bool                   { return false }
func (l *Link) Maintain()                         {}
func (l *Link) State() State                      { return State{} }
func (l *Link) OnReport(f func(State, time.Time)) {}
func (l *Link) SetRumble(left, right uint8)       {}
func (l *Link) Close()                            {}
