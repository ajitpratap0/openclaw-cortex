package main

import (
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/ajitpratap0/openclaw-cortex/internal/hookinstall"
)

// hookInstallCmd implements `openclaw-cortex hook install`.
func hookInstallCmd() *cobra.Command {
	var globalFlag bool
	var projectName string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install Claude Code hook configuration",
		Long: `Install writes the openclaw-cortex hook configuration into a Claude Code
settings.json file. By default it targets .claude/settings.json in the
current directory. Use --global to target ~/.claude/settings.json instead.

If the file already exists the hooks section is merged; other keys are preserved.`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Suppress unused warning — projectName is reserved for future use.
			_ = projectName

			// Determine target path.
			settingsPath, err := hookinstall.ResolveSettingsPath(globalFlag)
			if err != nil {
				return fmt.Errorf("resolving settings path: %w", err)
			}

			// Warn if openclaw-cortex is not in PATH.
			if _, lookErr := exec.LookPath(hookinstall.BinaryName); lookErr != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: %q not found in PATH — hooks will fail until the binary is installed\n",
					hookinstall.BinaryName,
				)
			}

			// Install / merge.
			changed, installErr := hookinstall.Install(settingsPath)
			if installErr != nil {
				return fmt.Errorf("hook install: %w", installErr)
			}

			// Report what happened.
			if changed {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "openclaw-cortex hook install: wrote hook configuration to %s\n", settingsPath)
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Added events: %s, %s\n", hookinstall.EventUserPromptSubmit, hookinstall.EventStop)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "openclaw-cortex hook install: %s already contains openclaw-cortex hooks — no changes made\n", settingsPath)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&globalFlag, "global", false, "Install into ~/.claude/settings.json instead of .claude/settings.json")
	cmd.Flags().StringVar(&projectName, "project", "", "Project name (reserved for future use)")
	_ = cmd.Flags().MarkHidden("project")

	return cmd
}
