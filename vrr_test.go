package vrr

import (
	"testing"
	"time"
)

func TestHarnessBasic(t *testing.T) {
	h := NewHarness(t, 4)
	defer h.Shutdown()

	// for i := 0; i < h.n; i++ {
	// 	h.cluster[i].replica.greetOthers()
	// }

	time.Sleep(8 * time.Second)
}
