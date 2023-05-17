package gh

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func GetGhToken() (string, error) {
	cmd := exec.Command("gh", "auth", "token")
	cmd.Env = os.Environ()
	ghToken, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("please login with gh cli: %v", err)
	}
	return strings.TrimSuffix(string(ghToken), "\n"), nil
}
