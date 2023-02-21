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

	"github.com/charmbracelet/log"
	"github.com/cli/cli/v2/git"
	"github.com/golonzovsky/github-multirepo/internal/gh"
	"github.com/google/go-github/v45/github"
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
	var rootCmd = &cobra.Command{Use: "multirepo"}

	rootCmd.PersistentFlags().String("owner", "ricardo-ch", "owner of the repo")
	rootCmd.PersistentFlags().String("target-dir", "/home/ax/project/ricardo-ch-full-org", "target for org checkout")

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
			repos, _, err := allOrgRepos(cmd.Context(), ownerFlag)
			if err != nil {
				return err
			}

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
			repos, count, err := allOrgRepos(cmd.Context(), ownerFlag)
			if err != nil {
				return err
			}
			log.Info("Total org repos:", "count", strconv.Itoa(count))

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
			repos, _, err := allOrgRepos(cmd.Context(), ownerFlag)
			if err != nil {
				return err
			}
			printLanguageStats(repos)
			return nil
		},
	}
	return cmd
}

func allOrgRepos(ctx context.Context, ownerFlag string) (<-chan *github.Repository, int, error) {
	client, err := gh.NewGithubClient(ctx, ownerFlag)
	if err != nil {
		return nil, 0, err
	}
	count, repositories, err := client.GetAllOrgRepos()
	log.Info("Total org repos:", "count", strconv.Itoa(count))
	return repositories, count, err
}

func cloneAllOrgRepos(cmd *cobra.Command, repos <-chan *github.Repository, targetDir string, client *git.Client) error {
	g, _ := errgroup.WithContext(cmd.Context())
	for i := 0; i < parallelWorkers; i++ {
		g.Go(func() error {
			for repo := range repos {
				targetLoc := targetDir + "/" + *repo.Name
				log.Info("Cloning", "repo", *repo.Name, "to", targetLoc)

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
				log.Info(fmt.Sprintf("Pulling %35s in %s", *repo.Name, targetLoc))
				branch := *repo.DefaultBranch
				url := *repo.CloneURL
				stderr := &bytes.Buffer{}
				stdout := &bytes.Buffer{}
				if err := client.Pull(cmd.Context(), url, branch,
					git.WithRepoDir(targetLoc), git.WithStderr(stderr), git.WithStdout(stdout), withForceGitColors()); err != nil {
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

func withForceGitColors() git.CommandModifier {
	return func(gc *git.Command) {
		// add -c after git executable (first index)
		gc.Args = append([]string{gc.Args[0], "-c", "color.ui=always"}, gc.Args[1:]...)
	}
}

func printLanguageStats(repos <-chan *github.Repository) {
	counts := make(map[string]int)
	for repo := range repos {
		if repo.Language == nil {
			continue
		}
		counts[*repo.Language]++
		log.Info(*repo.Name + " is in " + *repo.Language)
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
	for _, k := range keys {
		log.Info(k + ":" + strconv.Itoa(counts[k]))
	}
}
