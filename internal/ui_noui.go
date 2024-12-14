//go:build !ui

package internal

import (
	"context"

	"github.com/tcfw/otter/pkg/otter"
)

type uiConnector struct{}

func (u *uiConnector) Confirm(ctx context.Context, fields []otter.UIConfirmKV) <-chan bool {
	ch := make(chan bool)

	go func() {
		ch <- true
		close(ch)
	}()

	return ch
}

func (u *uiConnector) Notif(ctx context.Context, label string) error {
	return nil
}
