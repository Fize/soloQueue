package chatgpt

import (
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
		text := sd.String()
		if !strings.Contains(text, user.User.Sid.String()) || strings.Contains(text, ";;;WD)") || strings.Contains(text, ";;;BU)") {
			t.Fatalf("unexpected credential DACL: %s", text)
		}
	}
}
