package ui

import (
	"context"
	"time"

	"fyne.io/fyne/v2"
	fyneApp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/tcfw/otter/internal/version"
	"github.com/tcfw/otter/pkg/plugins"
	"github.com/tcfw/otter/pkg/protos/filedrop"
)

var (
	app fyne.App
)

func Run() {
	app.Run()
}

func Stop() {
	app.Driver().Quit()
}

func init() {
	app = fyneApp.New()
	// app.SetIcon(fyne.NewStaticResource("Otter", []byte{}))

	app.Lifecycle().SetOnStarted(func() {
		go func() {
			setActivationPolicy()
		}()
	})

	if desk, ok := app.(desktop.App); ok {
		m := fyne.NewMenu("Otter",
			&fyne.MenuItem{Label: "Otter " + version.Version(), Disabled: true},
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("New 5m token...", func() {
				for _, p := range plugins.LoadedPlugins() {
					if p.Name() == "filedrop" {
						client := p.Client().(filedrop.FileDropClient)
						tok, err := client.GenerateShareToken(nil, time.Now().Add(5*time.Minute))
						if err != nil {
							Alert(context.Background(), "Failed to generate new token")
							return
						}
						Alert(context.Background(), "Token: "+tok)
					}
				}
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				app.Quit()
			}))

		desk.SetSystemTrayMenu(m)
	}
}
