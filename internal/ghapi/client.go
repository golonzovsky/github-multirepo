package ghapi

import (
	"context"
	"sort"
	"strconv"

	"github.com/charmbracelet/log"
	"github.com/golonzovsky/github-multirepo/internal/ghcli"
	"github.com/google/go-github/v45/github"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
)

type client struct {
	owner       string
	ghApiClient *github.Client
}

func NewGithubClient(ctx context.Context, githubOrg string) (*client, error) {
	token, err := ghcli.GetGhToken()
	if err != nil {
		return nil, err
	}
	return &client{
		owner:       githubOrg,
		ghApiClient: initClient(ctx, token),
	}, nil
}

func initClient(ctx context.Context, githubAccessToken string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubAccessToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func (gc client) GetAllRepos(ctx context.Context) (int, <-chan *github.Repository, error) {
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

func (gc client) AllOrgRepos(ctx context.Context) (<-chan *github.Repository, int, error) {
	count, repositories, err := gc.GetAllRepos(ctx)
	log.Info("Total org repos:", "count", strconv.Itoa(count))
	return repositories, count, err
}

func PrintLanguageStats(repos <-chan *github.Repository) {
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
