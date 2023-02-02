package gh

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func AttemptReadToken() (string, error) {
	envToken := os.Getenv("GH_TOKEN")
	if envToken != "" {
		return envToken, nil
	}
	cmd := exec.Command("gh", "auth", "token")
	cmd.Env = os.Environ()
	ghToken, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("please login with gh cli or specify --gh-token flag or GH_TOKEN env var: %v", err)
	}
	return strings.TrimSuffix(string(ghToken), "\n"), nil
}
