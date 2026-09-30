package ds4w

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ExeVersion is the file version of an exe, such as "5.0.12.0", or "".
func ExeVersion(path string) string {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return ""
	}
	info := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&info[0])); err != nil {
		return ""
	}
	var fixed *windows.VS_FIXEDFILEINFO
	var n uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&info[0]), `\`, unsafe.Pointer(&fixed), &n); err != nil || fixed == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", fixed.FileVersionMS>>16, fixed.FileVersionMS&0xFFFF, fixed.FileVersionLS>>16, fixed.FileVersionLS&0xFFFF)
}
