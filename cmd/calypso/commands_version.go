package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"github.com/spf13/cobra"
)

// Populated via -ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("calypso %s\n", version)
			fmt.Printf("  commit: %s\n", commit)
			fmt.Printf("  date:   %s\n", date)
			fmt.Printf("  go:     %s\n", runtime.Version())
			if info, ok := debug.ReadBuildInfo(); ok {
				fmt.Printf("  module: %s\n", info.Main.Path)
			}
		},
	}
}
