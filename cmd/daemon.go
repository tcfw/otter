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

		o, err := internal.NewOtter(ctx)
		if err != nil {
			return err
		}
		defer o.Stop()

		fmt.Printf("PeerID: %s\n", o.P2P().ID().String())
		fmt.Println("Started...")

		go func() {
			for range time.NewTicker(1 * time.Minute).C {
				fmt.Printf("Connected to %d peers...\n", len(o.P2P().Network().Peers()))
			}
		}()

		go func() {
			sub, err := o.P2P().EventBus().Subscribe(&event.EvtLocalAddressesUpdated{})
			if err != nil {
				panic(err)
			}

			for evt := range sub.Out() {
				addrEvt := evt.(event.EvtLocalAddressesUpdated)
				fmt.Printf("\nℹ️ reachable address changes:\n")
				for _, addr := range addrEvt.Current {
					fmt.Printf("\t %s/p2p/%s\n", addr.Address.String(), o.P2P().ID().String())
				}
			}
		}()

		o.Bootstrap(nil)

		fmt.Println("Bootstrapped :)")

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c

		fmt.Println("Stopping...")

		return nil
	},
}
