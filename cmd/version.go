package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X github.com/nrr-project/nrr/cmd.Version=x.y.z"
var Version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the nrr version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("nrr %s\n", Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
