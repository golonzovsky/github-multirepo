package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/golonzovsky/github-multirepo/internal/ghapi"
	"github.com/golonzovsky/github-multirepo/internal/ghcli"
	"github.com/golonzovsky/github-multirepo/internal/gitrepo"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var (
	parallelWorkers = 10
)

func main() {
	log.SetLevel(log.DebugLevel)
	errLogger := log.New(os.Stderr)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	defer func() { signal.Stop(c) }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-c: // first signal, cancel context
			cancel()
		case <-ctx.Done():
		}
		<-c // second signal, hard exit
		errLogger.Warn("second interrupt, exiting")
		os.Exit(1)
	}()

	if err := NewRootCmd().ExecuteContext(ctx); err != nil {
		if err != context.Canceled {
			errLogger.Error(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func NewRootCmd() *cobra.Command {
	var rootCmd = &cobra.Command{Use: "multirepo"}

	rootCmd.PersistentFlags().String("owner", "ricardo-ch", "owner of the repo")
	rootCmd.PersistentFlags().String("target-dir", "", "target for org checkout")

	rootCmd.AddCommand(NewPullOrgCmd())
	rootCmd.AddCommand(NewCloneCmd())
	rootCmd.AddCommand(NewStatsCmd())
	rootCmd.AddCommand(NewPullFolderCmd())
	return rootCmd
}

func NewPullOrgCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "pull-org [owner] --target-dir [dir]",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			client, err := ghapi.NewGithubClient(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			repos, _, err := client.AllOrgRepos(cmd.Context())
			if err != nil {
				return err
			}

			targetDir, _ := cmd.Flags().GetString("target-dir")
			if err != nil || targetDir == "" {
				return fmt.Errorf("target-dir flag is required")
			}

			ghcli := ghcli.NewGithubCliClient()
			return ghcli.PullAllOrgRepos(cmd.Context(), repos, targetDir, parallelWorkers)
		},
	}
	return cmd
}

func NewPullFolderCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "pull",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			dir, err := os.Getwd()
			if err != nil {
				return err
			}

			dirs, err := gitrepo.GetFolderRepos(ctx, dir)
			if err != nil {
				return err
			}

			ghCliClient := ghcli.NewGithubCliClient()

			errgroup, ctx := errgroup.WithContext(ctx)
			for _, repoDir := range dirs {
				repoDir := repoDir
				fullDir := filepath.Join(dir, repoDir)
				errgroup.Go(func() error {
					return ghCliClient.PullRepo(ctx, repoDir, "", "", fullDir)
				})
			}
			return errgroup.Wait()
		},
	}
	return cmd
}

func NewCloneCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "clone [github-org]",
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			client, err := ghapi.NewGithubClient(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			repos, _, err := client.AllOrgRepos(cmd.Context())
			if err != nil {
				return err
			}

			targetDir, err := cmd.Flags().GetString("target-dir")
			if err != nil || targetDir == "" {
				return fmt.Errorf("target-dir flag is required")
			}

			ghCli := ghcli.NewGithubCliClient()
			return ghCli.CloneAllOrgRepos(cmd.Context(), repos, targetDir, parallelWorkers)
		},
	}
	return cmd
}

func NewStatsCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "stats",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			ownerFlag, _ := cmd.Flags().GetString("owner")

			client, err := ghapi.NewGithubClient(cmd.Context(), ownerFlag)
			if err != nil {
				return err
			}

			repos, _, err := client.AllOrgRepos(cmd.Context())
			if err != nil {
				return err
			}
			ghapi.PrintLanguageStats(repos)
			return nil
		},
	}
	return cmd
}
