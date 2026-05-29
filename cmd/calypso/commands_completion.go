package main

import (
	"os"

	"github.com/spf13/cobra"
)

func completionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Outputs a completion script for the specified shell.

To load completions:

  bash:
    source <(calypso completion bash)
    # Permanent: calypso completion bash > ~/.bash_completion

  zsh:
    source <(calypso completion zsh)
    # Permanent: calypso completion zsh > ~/.zfunc/_calypso

  fish:
    calypso completion fish > ~/.config/fish/completions/calypso.fish

  powershell:
    calypso completion powershell > calypso.ps1`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			switch args[0] {
			case "bash":
				err = cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				err = cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				err = cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				err = cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return err
		},
	}
}
