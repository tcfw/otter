package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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

		fmt.Println("Started...")

		go func() {
			for range time.NewTicker(1 * time.Minute).C {
				fmt.Printf("Connected to %d peers...\n", len(o.P2P().Network().Peers()))
			}
		}()

		// go func() {
		// 	sub, err := o.P2P().EventBus().Subscribe(&event.EvtLocalAddressesUpdated{})
		// 	if err != nil {
		// 		panic(err)
		// 	}

		// 	for evt := range sub.Out() {
		// 		fmt.Printf("reachable address changes: %+v", evt)
		// 	}
		// }()

		o.Bootstrap(nil)

		fmt.Println("Bootstrapped :)")

		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c

		fmt.Println("Stopping...")

		return nil
	},
}
