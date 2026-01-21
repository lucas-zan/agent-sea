package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"AgentEngine/cmd/ui"

	"golang.org/x/term"
)

// monitorCancellation puts the terminal in raw mode and listens for ESC key.
// It returns a cleanup function that must be called to restore terminal mode.
// If ESC is pressed twice, it calls cancel().
func monitorCancellation(ctx context.Context, cancel func()) func() {
	// check if stdin is a terminal
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}

	// switch to raw mode
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Printf("Warning: failed to enable raw mode for cancellation: %v\r\n", err)
		return func() {}
	}
	ui.IsRawMode = true

	// Channel to signal the monitor loop to stop
	stopCh := make(chan struct{})

	// Cleanup function
	cleanup := func() {
		close(stopCh)
		_ = term.Restore(fd, oldState)
		ui.IsRawMode = false
	}

	go func() {
		buf := make([]byte, 1)
		escCount := 0
		lastEscTime := time.Time{}

		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			default:
				// Read exactly one byte
				n, err := os.Stdin.Read(buf)
				if err != nil || n == 0 {
					return
				}

				key := buf[0]

				// Check for ESC (ASCII 27)
				if key == 27 {
					now := time.Now()
					// If it's been a while since last ESC, reset count
					if now.Sub(lastEscTime) > 3*time.Second {
						escCount = 0
					}

					escCount++
					lastEscTime = now

					if escCount == 1 {
						fmt.Print("\r\n⚠️  Press ESC again to stop...\r\n")
					} else if escCount >= 2 {
						fmt.Print("\r\n🛑 Cancelling...\r\n")
						cancel()
						return
					}
				} else {
					// Reset on any other key
					escCount = 0
				}
			}
		}
	}()

	return cleanup
}
