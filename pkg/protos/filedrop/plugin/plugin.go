//go:build plugin

package main

import (
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"github.com/tcfw/otter/pkg/protos/filedrop"
)

func main() {}

var (
	OtterPlugin plugins.Plugin = &FileDropPlugin{}
)

type FileDropPlugin struct{}

func (ap *FileDropPlugin) Name() string {
	return "filedrop"
}

func (ap *FileDropPlugin) Start(o otter.Otter) error {
	filedrop.Register(o)

	return nil
}

func (ap *FileDropPlugin) Stop() {}

func (ap *FileDropPlugin) Client() any { return nil }
