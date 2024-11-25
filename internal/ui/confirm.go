package ui

import (
	"context"

	"github.com/tcfw/otter/pkg/otter"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func Confirm(ctx context.Context, fields []otter.UIConfirmKV) <-chan bool {
	done := make(chan bool, 1)
	cancelled := make(chan struct{})

	w := app.NewWindow("Otter - Confirm")

	w.SetCloseIntercept(func() {
		close(cancelled)
	})

	objs := []fyne.CanvasObject{}

	for _, f := range fields {
		objs = append(objs,
			widget.NewLabelWithStyle(f.Label, fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewLabelWithStyle(f.Value, fyne.TextAlignCenter, fyne.TextStyle{}),
		)
	}

	btn := widget.NewButton("Accept", func() {
		done <- true
		close(done)
		close(cancelled)
		w.Close()
	})

	objs = append(objs, btn)

	w.SetContent(container.NewVBox(objs...))

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

	return done
}
