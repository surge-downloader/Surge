package cmd

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestExecuteGlobalShutdown_Once(t *testing.T) {
	var calls int32
	resetGlobalShutdownCoordinatorForTest(func() error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	t.Cleanup(func() { resetGlobalShutdownCoordinatorForTest(nil) })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := executeGlobalShutdown("test"); err != nil {
				t.Errorf("executeGlobalShutdown returned error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("shutdown function calls = %d, want 1", got)
	}
}
