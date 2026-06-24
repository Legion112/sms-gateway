//go:build !linux

package mm

import (
	"context"
	"fmt"
	"time"
)

// ModemInfo describes one modem discovered via ModemManager.
type ModemInfo struct {
	Index        int
	Path         string
	Manufacturer string
	Model        string
}

// ListModems is unavailable on non-Linux platforms.
func ListModems(_ context.Context, _ time.Duration) ([]ModemInfo, error) {
	return nil, fmt.Errorf("ModemManager discovery requires Linux")
}
