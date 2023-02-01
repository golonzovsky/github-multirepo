package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/golonzovsky/github-multirepo/internal/gh"
	"github.com/golonzovsky/github-multirepo/internal/logger"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
)

func main() {
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
		fmt.Fprintln(os.Stderr, "second interrupt, exiting")
		os.Exit(1)
	}()

	if err := NewRootCmd().ExecuteContext(ctx); err != nil {
		if err != context.Canceled {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func NewRootCmd() *cobra.Command {
	var (
		ghToken string
		owner   string
	)
	var rootCmd = &cobra.Command{
		Use:   "reposcan",
		Short: "reposcan helps to scan isopod versions in gh repos 🤖",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := gh.AttemptReadToken(ghToken)
			if err != nil {
				return err
			}
			client := gh.NewGithubClient(cmd.Context(), token, owner)
			count, names, err := client.GetAllOrgRepos()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "repos count in %s: %d", owner, count)
			fmt.Fprintln(os.Stderr, termenv.String(" Total org repos:").Foreground(logger.Blue), total)

			for repo := range names {
				fmt.Println(repo)
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&ghToken, "gh-token", "", "gh access token, can be set with env variable GH_TOKEN")
	rootCmd.Flags().StringVar(&owner, "owner", "ricardo-ch", "owner of the repo")

	return rootCmd
}
