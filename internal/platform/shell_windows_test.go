package platform

import (
	"errors"
	"fmt"
	"testing"
)

// TestDesktopShell reaches the desktop's Shell object and calls it, with
// nothing started: IsRestricted only reads a policy.
func TestDesktopShell(t *testing.T) {
	err := desktopShell(func(shell *comObject) error {
		if _, err := shell.dispID("ShellExecute"); err != nil {
			return err
		}
		group, restriction := bstr("Explorer"), bstr("NoRun")
		defer freeVariant(&group)
		defer freeVariant(&restriction)
		r, err := shell.invoke("IsRestricted", dispatchMethod, group, restriction)
		if err != nil {
			return err
		}
		defer freeVariant(&r)
		if r.vt != vtI4 {
			return fmt.Errorf("IsRestricted answered a VARIANT of type %d", r.vt)
		}
		return nil
	})
	if errors.Is(err, errNoDesktop) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
}
