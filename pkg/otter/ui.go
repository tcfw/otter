package otter

import "context"

type UIConfirmKV struct {
	Label string
	Value string
}

type UI interface {
	Confirm(ctx context.Context, fields []UIConfirmKV) <-chan bool
	Notif(ctx context.Context, label string) error
}
