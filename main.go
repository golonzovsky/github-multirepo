package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"

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
		owner     string
		targetDir string
	)
	var rootCmd = &cobra.Command{
		Use:           "multirepo",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			client, err := gh.NewGithubClient(cmd.Context(), owner)
			if err != nil {
				return err
			}
			count, repos, err := client.GetAllOrgRepos()
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, termenv.String(" Total org repos:", strconv.Itoa(count)).Foreground(logger.Blue))

			//printLanguageStats(repos)
			//return cloneAllOrgRepos(cmd, repos, targetDir, ghClient)
			return pullAllOrgRepos(cmd, repos, targetDir, gh.NewCliClient())
		},
	}

	rootCmd.PersistentFlags().StringVar(&owner, "owner", "ricardo-ch", "owner of the repo")
	rootCmd.PersistentFlags().StringVar(&targetDir, "target-dir", "/home/ax/project/ricardo-ch-full-org", "target for org checkout")
	rootCmd.AddCommand(NewPullCmd())
	rootCmd.AddCommand(NewCloneCmd())
	rootCmd.AddCommand(NewStatsCmd())

	return rootCmd
}

func NewPullCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "pull",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			ownerFlag, _ := cmd.Flags().GetString("owner")
			count, repos, err := allOrgRepos(cmd.Context(), ownerFlag)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, termenv.String(" Total org repos:", strconv.Itoa(count)).Foreground(logger.Blue))

			targetDir, _ := cmd.Flags().GetString("target-dir")
			return pullAllOrgRepos(cmd, repos, targetDir, gh.NewCliClient())
		},
	}
	return cmd
}

func NewCloneCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "clone",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			ownerFlag, _ := cmd.Flags().GetString("owner")
			count, repos, err := allOrgRepos(cmd.Context(), ownerFlag)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, termenv.String(" Total org repos:", strconv.Itoa(count)).Foreground(logger.Blue))

			targetDir, _ := cmd.Flags().GetString("target-dir")
			return cloneAllOrgRepos(cmd, repos, targetDir, gh.NewCliClient())
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
			count, repos, err := allOrgRepos(cmd.Context(), ownerFlag)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, termenv.String(" Total org repos:", strconv.Itoa(count)).Foreground(logger.Blue))
			printLanguageStats(repos)
			return nil
		},
	}
	return cmd
}

func allOrgRepos(ctx context.Context, ownerFlag string) (int, <-chan *github.Repository, error) {
	client, err := gh.NewGithubClient(ctx, ownerFlag)
	if err != nil {
		return 0, nil, err
	}
	return client.GetAllOrgRepos()
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
				msg := fmt.Sprintf("Pulling %35s in %s", *repo.Name, targetLoc)
				fmt.Fprintln(os.Stderr, termenv.String(msg).Foreground(logger.Green))
				branch := *repo.DefaultBranch
				url := *repo.CloneURL
				stderr := &bytes.Buffer{}
				stdout := &bytes.Buffer{}
				if err := client.Pull(cmd.Context(), url, branch,
					git.WithRepoDir(targetLoc), git.WithStderr(stderr), git.WithStdout(stdout)); err != nil {
					return fmt.Errorf("failed to pull %s: %w, with message: %s", *repo.Name, err, stderr.String())
				}
				out := stdout.String()
				if !strings.HasPrefix(out, "Already up to date.") {
					fmt.Print(out)
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
