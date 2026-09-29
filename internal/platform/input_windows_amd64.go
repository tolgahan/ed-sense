package platform

import "unsafe"

// SendInput fails when cbSize is not sizeof(INPUT), 40 bytes on amd64, so a
// wrong layout fails the build here instead.
var (
	_ [unsafe.Sizeof(input{}) - 40]byte
	_ [40 - unsafe.Sizeof(input{})]byte
)
