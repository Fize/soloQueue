package chatgpt

import (
	"fmt"
	"golang.org/x/sys/windows"
	"strings"
	"testing"
)

func TestCredentialWindowsProtectedDACL(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{m.dir, m.credentialPath()} {
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		control, _, err := sd.Control()
		if err != nil {
			t.Fatal(err)
		}
		if control&windows.SE_DACL_PROTECTED == 0 {
			t.Fatalf("DACL not protected: %s", path)
		}
		flags := ""
		if path == m.dir {
			flags = "OICI"
		}
		expected, err := windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)(A;%s;FA;;;BA)", flags, user.User.Sid.String(), flags, flags))
		if err != nil {
			t.Fatal(err)
		}
		text, want := sd.String(), expected.String()
		actualACE, expectedACE := strings.Index(text, "("), strings.Index(want, "(")
		if actualACE < 0 || expectedACE < 0 || text[actualACE:] != want[expectedACE:] {
			t.Fatalf("unexpected credential DACL: %s, want %s", text, want)
		}
	}
}
