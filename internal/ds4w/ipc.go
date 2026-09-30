package ds4w

// DS4Windows answers questions from its own command line ("-command
// Query.1.ProfileName"): the asker takes a mutex, makes a 256-byte shared
// memory block and an event, and sends the question to DS4Windows' window
// as WM_COPYDATA; DS4Windows writes the answer, in ASCII and without an
// end mark, into the block and sets the event. Query is EDSense's own
// asker. It never starts DS4Windows' command line, which reads the answer
// from the wrong block.

// Names are the kernel objects and the window title of that exchange.
// Tests use their own.
type Names struct {
	ClassBlock  string // shared memory with the window's class name
	ResultBlock string // shared memory for the answer
	Mutex       string // one question at a time
	Ready       string // the event DS4Windows sets when it has answered
	Title       string // of DS4Windows' window
}

// DS4WindowsNames are DS4Windows' own.
var DS4WindowsNames = Names{
	ClassBlock:  "DS4Windows_IPCClassName.dat",
	ResultBlock: "DS4Windows_IPCResultData.dat",
	Mutex:       "DS4Windows_IPCResultData_SingleTaskMtx",
	Ready:       "DS4Windows_IPCResultData_ReadyEvent",
	Title:       "DS4Windows",
}

// Question properties.
const (
	PropProfile = "ProfileName" // the profile the slot uses now, temporary ones too
	PropOutput  = "OutContType" // the emulated controller
)
