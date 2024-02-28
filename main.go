package main

import (
	"context"
	"errors"
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
		if !errors.Is(err, context.Canceled) {
			errLogger.Error(err)
		}
		os.Exit(1)
	}
}

func NewRootCmd() *cobra.Command {
	var cmd = &cobra.Command{Use: "multirepo"}

	cmd.PersistentFlags().Int("parallel-workers", 500, "number of parallel workers")

	cmd.AddCommand(NewPullOrgCmd())
	cmd.AddCommand(NewCloneCmd())
	cmd.AddCommand(NewStatsCmd())
	cmd.AddCommand(NewPullFolderCmd())
	return cmd
}

func NewPullOrgCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "pull-org [owner]",
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

			targetDir, err := targetDirOrCurrentDir(cmd)
			if err != nil {
				return err
			}

			parallelWorkers, _ := cmd.Flags().GetInt("parallel-workers")

			ghcli := ghcli.NewGithubCliClient()
			return ghcli.PullAllOrgRepos(cmd.Context(), repos, targetDir, parallelWorkers)
		},
	}
	cmd.Flags().String("target-dir", "", "target for org checkout")
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

			eg, ctx := errgroup.WithContext(ctx)
			for _, repoDir := range dirs {
				repoDir := repoDir
				fullDir := filepath.Join(dir, repoDir)
				eg.Go(func() error {
					return ghCliClient.PullRepo(ctx, repoDir, "", "", fullDir)
				})
			}
			return eg.Wait()
		},
	}
	return cmd
}

func NewCloneCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "clone [github-owner] --target-dir [dir]",
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

			targetDir, err := targetDirOrCurrentDir(cmd)
			if err != nil {
				return err
			}

			parallelWorkers, _ := cmd.Flags().GetInt("parallel-workers")

			ghCli := ghcli.NewGithubCliClient()
			return ghCli.CloneAllOrgRepos(cmd.Context(), repos, targetDir, parallelWorkers)
		},
	}
	cmd.Flags().String("target-dir", "", "target for org checkout")
	return cmd
}

func targetDirOrCurrentDir(cmd *cobra.Command) (string, error) {
	targetDir, _ := cmd.Flags().GetString("target-dir")
	if targetDir != "" {
		return targetDir, nil
	}
	currentDir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	log.Info("--target-dir flag is not provided, using current directory", "dir", currentDir)
	return currentDir, err
}

func NewStatsCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "stats [github-owner]",
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
			ghapi.PrintLanguageStats(repos)
			return nil
		},
	}
	return cmd
}
