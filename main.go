package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"

	"github.com/cli/cli/v2/git"
	"github.com/golonzovsky/github-multirepo/internal/gh"
	"github.com/golonzovsky/github-multirepo/internal/logger"
	"github.com/google/go-github/v45/github"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var (
	parallelWorkers = 10
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
		ghToken   string
		owner     string
		targetDir string
	)
	var rootCmd = &cobra.Command{
		Use:           "multirepo",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			token, err := gh.AttemptReadToken(ghToken)
			if err != nil {
				return err
			}
			client := gh.NewGithubClient(cmd.Context(), token, owner)
			count, repos, err := client.GetAllOrgRepos()
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr,
				termenv.String(" Total org repos:", strconv.Itoa(count)).Foreground(logger.Blue),
			)

			ghClient := &git.Client{
				Stderr: os.Stderr,
				Stdin:  os.Stdin,
				Stdout: os.Stdout,
			}

			//printLanguageStats(repos)
			//return cloneAllOrgRepos(cmd, repos, targetDir, ghClient)
			return pullAllOrgRepos(cmd, repos, targetDir, ghClient)

			//return nil
		},
	}

	rootCmd.Flags().StringVar(&ghToken, "gh-token", "", "gh access token, can be set with env variable GH_TOKEN")
	rootCmd.Flags().StringVar(&owner, "owner", "ricardo-ch", "owner of the repo")
	rootCmd.Flags().StringVar(&targetDir, "target-dir", "/home/ax/project/ricardo-ch-full-org", "target for org checkout")

	return rootCmd
}

func cloneAllOrgRepos(cmd *cobra.Command, repos <-chan *github.Repository, targetDir string, client *git.Client) error {
	g, _ := errgroup.WithContext(cmd.Context())
	for i := 0; i < parallelWorkers; i++ {
		g.Go(func() error {
			for repo := range repos {
				targetLoc := targetDir + "/" + *repo.Name
				fmt.Fprintln(os.Stderr, termenv.String("Cloning", *repo.Name, "to", targetLoc).Foreground(logger.Green))

				if _, err := client.Clone(cmd.Context(), *repo.CloneURL, []string{targetLoc}); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}

func pullAllOrgRepos(cmd *cobra.Command, repos <-chan *github.Repository, targetDir string, client *git.Client) error {
	g, _ := errgroup.WithContext(cmd.Context())
	for i := 0; i < parallelWorkers; i++ {
		g.Go(func() error {
			for repo := range repos {
				targetLoc := targetDir + "/" + *repo.Name
				fmt.Fprintln(os.Stderr, termenv.String("Pulling", *repo.Name, "in", targetLoc).Foreground(logger.Green))
				branch := *repo.DefaultBranch
				url := *repo.CloneURL
				if err := client.Pull(cmd.Context(), url, branch, git.WithRepoDir(targetLoc)); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}

func printLanguageStats(repos <-chan *github.Repository) {
	counts := make(map[string]int)
	for repo := range repos {
		if repo.Language == nil {
			continue
		}
		counts[*repo.Language]++
		fmt.Fprintln(os.Stderr, termenv.String(*repo.Name, "is", *repo.Language).Foreground(logger.Green))
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
	for _, k := range keys {
		fmt.Fprintln(os.Stderr, termenv.String(k, ":", strconv.Itoa(counts[k])).Foreground(logger.Blue))
	}
}
