package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/cli/cli/v2/git"
	"github.com/golonzovsky/github-multirepo/internal/cliclient"
	"github.com/golonzovsky/github-multirepo/internal/gh"
	"github.com/golonzovsky/github-multirepo/internal/gitrepo"
	"github.com/google/go-github/v45/github"
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
	rootCmd.PersistentFlags().String("target-dir", "/home/ax/project/ricardo-ch-full-org", "target for org checkout")

	rootCmd.AddCommand(NewPullOrgCmd())
	rootCmd.AddCommand(NewCloneCmd())
	rootCmd.AddCommand(NewStatsCmd())
	rootCmd.AddCommand(NewPullFolderCmd())
	return rootCmd
}

// todo this should pull from the current folder and not from the target dir with owner flag
func NewPullOrgCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "pull-org [owner] --target-dir [dir]",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			repos, _, err := allOrgRepos(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			targetDir, _ := cmd.Flags().GetString("target-dir")
			return pullAllOrgRepos(cmd.Context(), repos, targetDir, gh.NewGithubCliClient())
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

			githubCliClient := gh.NewGithubCliClient()

			errgroup, ctx := errgroup.WithContext(ctx)
			for _, repoDir := range dirs {
				repoDir := repoDir
				fullDir := filepath.Join(dir, repoDir)
				errgroup.Go(func() error {
					return cliclient.PullRepo(ctx, githubCliClient, repoDir, "", "", fullDir)
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

			repos, _, err := allOrgRepos(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			targetDir, err := cmd.Flags().GetString("target-dir")
			if err != nil {
				return fmt.Errorf("target-dir flag is required")
			}
			return cloneAllOrgRepos(cmd, repos, targetDir, gh.NewGithubCliClient())
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

func allOrgRepos(ctx context.Context, owner string) (<-chan *github.Repository, int, error) {
	client, err := gh.NewGithubClient(ctx, owner)
	if err != nil {
		return nil, 0, err
	}
	count, repositories, err := client.GetAllRepos(ctx)
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

				cmd, err := client.AuthenticatedCommand(cmd.Context(), "clone", *repo.CloneURL, targetLoc)
				if err != nil {
					return err
				}
				stdErr := &bytes.Buffer{}
				cmd.Stderr = stdErr
				if err := cmd.Run(); err != nil {
					if strings.Contains(stdErr.String(), "already exists and is not an empty directory") {
						log.Debug("Repo already exists, skipping", "repo", *repo.Name)
						continue
					}
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}

func pullAllOrgRepos(ctx context.Context, repos <-chan *github.Repository, targetDir string, client *git.Client) error {
	g, _ := errgroup.WithContext(ctx)
	for i := 0; i < parallelWorkers; i++ {
		g.Go(func() error {
			for repo := range repos {
				if repo.Archived != nil && *repo.Archived {
					log.Debug("Repo is archived, skipping", "repo", *repo.Name)
					continue
				}
				err := cliclient.PullRepo(ctx, client, *repo.Name, *repo.DefaultBranch, *repo.CloneURL, targetDir+"/"+*repo.Name)
				if err != nil {
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
