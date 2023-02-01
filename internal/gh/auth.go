package gh

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func AttemptReadToken(token string) (string, error) {
	if token != "" {
		return token, nil
	}

	envToken := os.Getenv("GH_TOKEN")
	if envToken != "" {
		return envToken, nil
	}

	ghToken, err := attemptReadGhCliToken()
	if err != nil {
		return ghToken, fmt.Errorf("please login with gh cli or specify --gh-token flag or GH_TOKEN env var: %v", err)
	}
	return ghToken, nil
}

func attemptReadGhCliToken() (string, error) {
	data, err := readHostConfig()
	if err != nil {
		return "", fmt.Errorf("failed to read ~/.config/github/hosts.yml: %v", err)
	}
	//var root yaml.Node
	type HostConfig struct {
		User  string `yaml:"user"`
		Token string `yaml:"oauth_token"`
	}
	var cfg map[string]HostConfig
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return "", fmt.Errorf("failed to parse ~/.config/gh/hosts.yml: %v", err)
	}
	hostConfig, ok := cfg["github.com"]
	if !ok {
		return "", fmt.Errorf("failed to find github.com credentials in ~/.config/gh/hosts.yml")
	}
	return hostConfig.Token, nil
}

func readHostConfig() ([]byte, error) {
	usr, _ := user.Current()
	f, err := os.Open(filepath.Join(usr.HomeDir, ".config/gh/hosts.yml"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return data, nil
}
