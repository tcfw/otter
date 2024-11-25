package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tcfw/otter/internal"
	"github.com/tcfw/otter/internal/ui"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func init() {
	rootCmd.AddCommand(daemonCmd)
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the Otter daemon service",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := viper.ReadInConfig(); err != nil {
			return err
		}

		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		logger := zap.Must(config.Build())

		o, err := internal.NewOtter(ctx, logger)
		if err != nil {
			return err
		}
		defer o.Stop()

		logger = logger.Named("daemon")

		logger.Info("Started", zap.Any("peerId", o.P2P().ID().String()))

		go func() {
			for range time.NewTicker(1 * time.Minute).C {
				logger.Info("peer count", zap.Any("n", len(o.P2P().Network().Peers())))
			}
		}()

		go func() {
			sub, err := o.P2P().EventBus().Subscribe(&event.EvtLocalAddressesUpdated{})
			if err != nil {
				panic(err)
			}

			for evt := range sub.Out() {
				addrEvt := evt.(event.EvtLocalAddressesUpdated)
				addrs := []zapcore.Field{}
				for _, addr := range addrEvt.Current {
					addrs = append(addrs, zap.Any("addr", fmt.Sprintf("%s/p2p/%s", addr.Address.String(), o.P2P().ID().String())))
				}
				logger.Info("reachable address changes", addrs...)
			}
		}()

		go func() {
			o.Bootstrap(nil)

			logger.Info("Bootstrapped")
		}()

		go func() {
			c := make(chan os.Signal, 1)
			signal.Notify(c, os.Interrupt, syscall.SIGTERM)
			<-c
			ui.Stop()
		}()

		ui.Run()

		logger.Info("Stopping...")

		return nil
	},
}
