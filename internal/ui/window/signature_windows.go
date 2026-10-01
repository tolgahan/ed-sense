package window

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// about checks this exe's signature with Windows, offline: no revocation
// check and no download, so the window reaches nothing on the internet.
func about() About {
	exe, err := os.Executable()
	if err != nil {
		return About{Signature: NotSigned}
	}
	thumb, err := signerThumbprint(exe)
	if err != nil || thumb == "" {
		return About{Signature: NotSigned}
	}
	if verify(exe) != nil {
		return About{Signature: SignedInvalid, Thumbprint: thumb}
	}
	return About{Signature: SignedValid, Thumbprint: thumb}
}

func verify(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	file := &windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: p}
	data := &windows.WinTrustData{
		Size:                            uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:                        windows.WTD_UI_NONE,
		RevocationChecks:                windows.WTD_REVOKE_NONE,
		UnionChoice:                     windows.WTD_CHOICE_FILE,
		StateAction:                     windows.WTD_STATEACTION_VERIFY,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(file),
		ProvFlags:                       windows.WTD_CACHE_ONLY_URL_RETRIEVAL | windows.WTD_REVOCATION_CHECK_NONE,
	}
	err = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	return err
}
