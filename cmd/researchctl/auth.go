package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
)

func newAuthCommand() *cobra.Command {
	command := &cobra.Command{Use: "auth", Short: "Manage operator authentication", Args: cobra.NoArgs}
	var path string
	generate := &cobra.Command{Use: "generate", Short: "Generate a private operator token file", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if path == "" {
			return errors.New("auth generate requires --file")
		}
		token, err := operatorauth.Generate()
		if err != nil {
			return err
		}
		if err := token.WriteFile(path); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "operator token written to %s\n", path)
		return nil
	}}
	generate.Flags().StringVar(&path, "file", "", "new owner-only operator token file")
	command.AddCommand(generate)
	return command
}
