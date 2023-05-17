package gh

import (
	"context"
	"os"

	"github.com/charmbracelet/log"
	"github.com/cli/cli/v2/git"
	"github.com/google/go-github/v45/github"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
)

func InitClient(ctx context.Context, githubAccessToken string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubAccessToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

type GithubClient struct {
	owner string

	ghApiClient *github.Client
	ghCliClient *git.Client
}

func NewGithubClient(ctx context.Context, githubOrg string) (*GithubClient, error) {
	token, err := GetGhToken()
	if err != nil {
		return nil, err
	}
	return &GithubClient{
		owner:       githubOrg,
		ghApiClient: InitClient(ctx, token),
		ghCliClient: NewGithubCliClient(),
	}, nil
}

func NewGithubCliClient() *git.Client {
	return &git.Client{
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
	}
}

func (gc GithubClient) GetAllRepos(ctx context.Context) (int, <-chan *github.Repository, error) {
	org, _, err := gc.ghApiClient.Organizations.Get(ctx, gc.owner)
	if err != nil {
		return 0, nil, err
	}

	const perPage = 50
	totalRepos := *org.OwnedPrivateRepos + *org.PublicRepos
	numPages := (totalRepos + perPage - 1) / perPage
	repos := make(chan *github.Repository)
	g, _ := errgroup.WithContext(ctx)
	for p := 1; p <= numPages; p++ {
		page := p
		g.Go(func() error {
			opt := &github.RepositoryListByOrgOptions{
				ListOptions: github.ListOptions{Page: page, PerPage: perPage},
				Type:        "all",
			}
			repoPage, _, err := gc.ghApiClient.Repositories.ListByOrg(ctx, gc.owner, opt)
			if err != nil {
				return err
			}
			for _, repo := range repoPage {
				if !*repo.Archived {
					repos <- repo
				}
			}
			return nil
		})
	}
	go func() {
		err := g.Wait()
		if err != nil && err != context.Canceled {
			log.Error("error fetching org repos:", "err", err)
		}
		close(repos)
	}()
	return totalRepos, repos, nil
}

func (gc GithubClient) Clone(ctx context.Context, url string, targetLocation string) error {
	_, err := gc.ghCliClient.Clone(ctx, url, []string{targetLocation})
	return err
}
