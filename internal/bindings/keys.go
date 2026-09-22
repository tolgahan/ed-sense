package bindings

import (
	"strconv"
	"strings"
)

var namedKeys = map[string]int{
	"Space": 0x20, "Tab": 0x09, "Enter": 0x0D, "Backspace": 0x08, "Escape": 0x1B,
	"Home": 0x24, "End": 0x23, "PageUp": 0x21, "PageDown": 0x22, "Insert": 0x2D, "Delete": 0x2E,
	"UpArrow": 0x26, "DownArrow": 0x28, "LeftArrow": 0x25, "RightArrow": 0x27,
	"LeftShift": 0xA0, "RightShift": 0xA1, "LeftControl": 0xA2, "RightControl": 0xA3, "LeftAlt": 0xA4, "RightAlt": 0xA5,
	"CapsLock": 0x14, "Minus": 0xBD, "Equals": 0xBB, "LeftBracket": 0xDB, "RightBracket": 0xDD,
	"SemiColon": 0xBA, "Apostrophe": 0xDE, "Comma": 0xBC, "Period": 0xBE, "Slash": 0xBF, "BackSlash": 0xDC, "Grave": 0xC0,
	"Numpad_Add": 0x6B, "Numpad_Subtract": 0x6D, "Numpad_Multiply": 0x6A, "Numpad_Divide": 0x6F, "Numpad_Decimal": 0x6E, "Numpad_Enter": 0x0D,
}

// virtualKey maps Elite's keyboard key name (Key_F9, Key_A ...) to a
// Windows virtual-key code, or 0.
func virtualKey(elite string) int {
	k := strings.TrimPrefix(elite, "Key_")
	if len(k) == 1 && (k[0] >= 'A' && k[0] <= 'Z' || k[0] >= '0' && k[0] <= '9') {
		return int(k[0])
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(k, "F")); err == nil && strings.HasPrefix(k, "F") && n >= 1 && n <= 24 {
		return 0x6F + n
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(k, "Numpad_")); err == nil && strings.HasPrefix(k, "Numpad_") && n >= 0 && n <= 9 {
		return 0x60 + n
	}
	return namedKeys[k]
}
