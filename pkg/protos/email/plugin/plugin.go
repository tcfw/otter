//go:build plugin

package main

import (
	"github.com/tcfw/otter/pkg/otter"
	"github.com/tcfw/otter/pkg/plugins"
	"github.com/tcfw/otter/pkg/protos/email"
)

func main() {}

var (
	OtterPlugin plugins.Plugin = &EmailPlugin{}
)

type EmailPlugin struct{}

func (ep *EmailPlugin) Name() string {
	return "email"
}

func (ep *EmailPlugin) Start(o otter.Otter) error {
	email.Register(o)

	return nil
}

func (ep *EmailPlugin) Stop() {}

func (ep *EmailPlugin) Client() any { return nil }
