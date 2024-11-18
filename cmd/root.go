package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tcfw/otter/internal"
)

var rootCmd = &cobra.Command{
	Use:   "otter",
	Short: "Otter is an extendable Personal Data Server",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of Otter",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Otter " + internal.FullVersion())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
