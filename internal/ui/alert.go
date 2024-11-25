package ui

import (
	"context"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

func Alert(ctx context.Context, msg string) {
	cancelled := make(chan struct{})

	w := app.NewWindow("Otter")

	w.SetCloseIntercept(func() {
		close(cancelled)
	})

	w.SetContent(container.NewCenter(widget.NewLabel(msg), widget.NewButtonWithIcon("copy content", theme.ContentCopyIcon(), func() {
		w.Clipboard().SetContent(msg)
	})))

	w.Show()
	w.CenterOnScreen()
	w.RequestFocus()

	go func() {
		defer w.Close()

		select {
		case <-cancelled:
		case <-ctx.Done():
		}
	}()
}
