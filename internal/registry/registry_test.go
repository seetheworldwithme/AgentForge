package registry

import "testing"

func TestSetupRegistersSystemInfo(t *testing.T) {
	r := Default()
	if _, ok := r.Get("get_system_info"); !ok {
		t.Fatal("expected get_system_info tool registered")
	}
	if len(r.List()) < 1 {
		t.Fatal("expected at least 1 builtin tool")
	}
}
