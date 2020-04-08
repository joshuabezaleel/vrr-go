package vrr

import "testing"

func TestHarnessBasic(t *testing.T) {
	h := NewHarness(t, 4)
	defer h.Shutdown()
}
