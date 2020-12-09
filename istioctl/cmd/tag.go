package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

func tagCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tag",
		Short:   "Command group used to interact with revision-tags",
		Example: "istioctl tag apply prod --revision 1-8-0",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}

			return nil
		},
	}

	cmd.AddCommand(tagApplyCommand())
	cmd.AddCommand(tagRemoveCommand())
	cmd.AddCommand(tagListCommand())

	return cmd
}

func tagApplyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apply",
		Short:   "Create or redirect an existing revision tag",
		Example: "istioctl tag apply prod --revision 1-8-0",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}

			return nil
		},
	}

	return cmd
}

func tagListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List existing revision tags and their corresponding revisions",
		Example: "istioctl tag apply prod --revision 1-8-0",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}

			return nil
		},
	}

	return cmd
}

func tagRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an existing revision tag",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.HelpFunc()(cmd, args)
			if len(args) != 0 {
				return fmt.Errorf("unknown subcommand %q", args[0])
			}

			return nil
		},
	}

	return cmd
}
