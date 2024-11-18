//go:build plugin

package main

import (
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"github.com/tcfw/otter/pkg/protos/activitypub"
)

func main() {}

var (
	OtterPlugin plugins.Plugin = &ActivityPubPlugin{}
)

type ActivityPubPlugin struct{}

func (ap *ActivityPubPlugin) Name() string {
	return "activityPub"
}

func (ap *ActivityPubPlugin) Start(o otter.Otter) error {
	activitypub.Register(o)

	return nil
}

func (ap *ActivityPubPlugin) Stop() {}
