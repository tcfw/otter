package ui

import (
	"context"

	"fyne.io/fyne/v2"
)

func Notif(ctx context.Context, label string) {
	app.SendNotification(&fyne.Notification{
		Title:   label,
		Content: "",
	})
}
