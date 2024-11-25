package internal

import (
	"context"

	"github.com/tcfw/otter/internal/ui"
	"github.com/tcfw/otter/pkg/otter"
)

type uiConnector struct{}

func (u *uiConnector) Confirm(ctx context.Context, fields []otter.UIConfirmKV) <-chan bool {
	return ui.Confirm(ctx, fields)
}

func (u *uiConnector) Notif(ctx context.Context, label string) error {
	ui.Notif(ctx, label)

	return nil
}
