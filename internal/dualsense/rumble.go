package dualsense

import "hash/crc32"

// RumbleReport builds an output report that sets only the rumble motors
// (valid_flag0: compatible vibration + haptics select), so DSX's lightbar,
// LED and trigger settings are left alone. DSX's "Rumble to Haptics" turns
// the motor levels into haptics on the real controller.
func RumbleReport(outLen int, left, right uint8) []byte {
	const flags = 0x03
	if outLen >= 78 { // Bluetooth: report 0x31 with a CRC over a 0xA2 seed
		b := make([]byte, outLen)
		b[0], b[1] = 0x31, 0x02
		b[2], b[4], b[5] = flags, right, left
		crc := crc32.NewIEEE()
		crc.Write([]byte{0xA2})
		crc.Write(b[:outLen-4])
		v := crc.Sum32()
		b[outLen-4], b[outLen-3], b[outLen-2], b[outLen-1] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
		return b
	}
	b := make([]byte, max(outLen, 48))
	b[0], b[1], b[3], b[4] = 0x02, flags, right, left
	return b
}
