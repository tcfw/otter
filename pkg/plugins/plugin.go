package plugins

import (
	"errors"

	"github.com/tcfw/otter/pkg/otter"
)

type Plugin interface {
	Start(otter.Otter) error
	Stop()
}

func LoadAndStart(path string, o otter.Otter) error {
	return errors.New("unimplemented")
}
