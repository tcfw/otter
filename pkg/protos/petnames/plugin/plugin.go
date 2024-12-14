//go:build plugin

package main

import (
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"github.com/tcfw/otter/pkg/protos/activitypub"
)

func main() {}

var (
	OtterPlugin plugins.Plugin = &PetnamesPlugin{}
)

type PetnamesPlugin struct{}

func (ap *PetnamesPlugin) Name() string {
	return "petnames"
}

func (ap *PetnamesPlugin) Start(o otter.Otter) error {
	activitypub.Register(o)

	return nil
}

func (ap *PetnamesPlugin) Stop()       {}
func (ap *PetnamesPlugin) Client() any { return nil }
