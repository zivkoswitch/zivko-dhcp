//go:build windows

package runtime

import (
	"context"
	"os"
	"os/signal"
)

func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}
