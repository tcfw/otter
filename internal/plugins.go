package internal

import (
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"github.com/tcfw/otter/pkg/protos/activitypub"
	"github.com/tcfw/otter/pkg/protos/filedrop"
	"go.uber.org/zap"
)

var (
	builtins = []plugins.Plugin{
		&BuildinPlugin{NameF: "filedrop", StartF: func(o otter.Otter) error { filedrop.Register(o); return nil }, ClientF: func() any { return filedrop.Client() }},
		&BuildinPlugin{NameF: "activitypub", StartF: func(o otter.Otter) error { activitypub.Register(o); return nil }}, //ClientF: func() any { return activitypub.Client() }},
	}
)

func startBuiltinPlugins(o otter.Otter) error {
	for _, plugin := range builtins {
		if err := plugin.Start(o); err != nil {
			o.Logger("plugins").Error("loading plugin", zap.Any("name", plugin.Name()))
		}
		plugins.AddBuiltIn(plugin)
	}

	return nil
}

type BuildinPlugin struct {
	NameF   string
	StartF  func(o otter.Otter) error
	StopF   func()
	ClientF func() any
}

func (bp *BuildinPlugin) Name() string { return bp.NameF }

func (bp *BuildinPlugin) Start(o otter.Otter) error {
	if bp.StartF != nil {
		return bp.StartF(o)
	}

	return nil
}

func (bp *BuildinPlugin) Stop() {
	if bp.StopF != nil {
		bp.StopF()
	}
}

func (bp *BuildinPlugin) Client() any {
	if bp.ClientF != nil {
		return bp.ClientF()
	}

	return nil
}
