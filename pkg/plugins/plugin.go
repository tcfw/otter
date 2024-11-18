package plugins

import (
	"fmt"
	"plugin"

	"github.com/tcfw/otter/pkg/otter"
)

var (
	loadedPlugins = map[string]Plugin{}
)

type Plugin interface {
	Start(otter.Otter) error
	Name() string
	Stop()
}

func LoadedPlugins() []Plugin {
	a := make([]Plugin, 0, len(loadedPlugins))

	for _, p := range loadedPlugins {
		a = append(a, p)
	}

	return a
}

func LoadAndStart(path string, o otter.Otter) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered from panic during plugin load: %+v", r)
		}
	}()

	pf, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("loading plugin library: %w", err)
	}

	sym, err := pf.Lookup("OtterPlugin")
	if err != nil {
		return fmt.Errorf("missing plugin entry symbol: %w", err)
	}

	pe, ok := sym.(*Plugin)
	if !ok {
		return fmt.Errorf("plugin symbol not expected type, got %T, expected plugins.Plugin", sym)
	}

	pt := *pe

	if err := pt.Start(o); err != nil {
		return fmt.Errorf("starting plugin: %w", err)
	}

	loadedPlugins[pt.Name()] = pt

	fmt.Printf("Loaded plugin: %s", pt.Name())

	return nil
}
