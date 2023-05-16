package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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

	rootCmd.AddCommand(NewPullCmd())
	rootCmd.AddCommand(NewCloneCmd())
	rootCmd.AddCommand(NewStatsCmd())
	rootCmd.AddCommand(NewPullFolderCmd())
	return rootCmd
}

// todo this should pull from the current folder and not from the target dir with owner flag
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
			return pullAllOrgRepos(cmd.Context(), repos, targetDir, gh.NewGithubCliClient())
		},
	}
	return cmd
}

func NewPullFolderCmd() *cobra.Command {
	var cmd = &cobra.Command{
		Use:           "pull-folder",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			ctx := cmd.Context()
			dir, err := os.Getwd()
			if err != nil {
				return err
			}

			dirs, err := getFolderRepos(ctx, dir)
			if err != nil {
				return err
			}

			githubCliClient := gh.NewGithubCliClient()

			for _, repoDir := range dirs {
				fullDir := filepath.Join(dir, repoDir)
				err := pullRepo(ctx, githubCliClient, repoDir, "", "", fullDir)
				if err != nil {
					return err
				}
			}
			return nil
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

func getFolderRepos(ctx context.Context, dir string) ([]string, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, dirEntry := range dirEntries {
		fullPath := filepath.Join(dir, dirEntry.Name())
		if dirEntry.IsDir() && isGitRepo(ctx, fullPath) {
			dirs = append(dirs, dirEntry.Name())
		}
	}
	return dirs, nil
}

func isGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	err := cmd.Run()
	return err == nil
}

func allOrgRepos(ctx context.Context, owner string) (<-chan *github.Repository, int, error) {
	client, err := gh.NewGithubClient(ctx, owner)
	if err != nil {
		return nil, 0, err
	}
	count, repositories, err := client.GetAllRepos()
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
				err := pullRepo(ctx, client, *repo.Name, *repo.DefaultBranch, *repo.CloneURL, targetDir+"/"+*repo.Name)
				if err != nil {
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}

func pullRepo(ctx context.Context, client *git.Client, repoName string, branch string, url string, targetFolder string) error {
	log.Info(fmt.Sprintf("Pulling %35s in %s", repoName, targetFolder))
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	if err := client.Pull(ctx, url, branch,
		git.WithRepoDir(targetFolder), git.WithStderr(stderr), git.WithStdout(stdout), withForceGitColors()); err != nil {
		errorMsg := stderr.String()
		if strings.Contains(errorMsg, "couldn't find remote ref") {
			log.Warn("No default branch found, skipping", "repo", repoName)
			return nil
		}
		return fmt.Errorf("failed to pull %s: %w, with message: %s", repoName, err, errorMsg)
	}
	out := stdout.String()
	if !strings.HasPrefix(out, "Already up to date.") {
		fmt.Print(out)
	}
	return nil
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
