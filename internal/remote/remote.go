// Package remote lists Epitech repos on GitHub through the gh CLI, so epi
// reuses your existing gh login instead of handling tokens itself.
package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cempack/epi/internal/epitech"
)

type Repo struct {
	epitech.Repo
	SSHURL    string    `json:"sshUrl"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
}

func gh(ctx context.Context, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "gh", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh not available: %w", err)
	}
	return out, nil
}

// Orgs returns Epitech-looking orgs you belong to, plus any extra owners
// (taken from local remotes), so a new school year's org is found on its own.
func Orgs(ctx context.Context, extra []string) []string {
	set := map[string]bool{}
	for _, o := range extra {
		if o != "" {
			set[o] = true
		}
	}
	if out, err := gh(ctx, "api", "user/orgs", "--jq", ".[].login"); err == nil {
		for _, o := range strings.Fields(string(out)) {
			if strings.Contains(strings.ToLower(o), "epitech") {
				set[o] = true
			}
		}
	}
	var orgs []string
	for o := range set {
		orgs = append(orgs, o)
	}
	sort.Strings(orgs)
	return orgs
}

// List fetches every Epitech-shaped repo from the orgs, newest first.
func List(ctx context.Context, orgs []string) ([]Repo, error) {
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		all   []Repo
		first error
	)
	for _, org := range orgs {
		wg.Add(1)
		go func(org string) {
			defer wg.Done()
			out, err := gh(ctx, "repo", "list", org, "--limit", "500", "--json", "name,sshUrl,url,createdAt")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if first == nil {
					first = err
				}
				return
			}
			var raw []struct {
				Name      string    `json:"name"`
				SSHURL    string    `json:"sshUrl"`
				URL       string    `json:"url"`
				CreatedAt time.Time `json:"createdAt"`
			}
			if json.Unmarshal(out, &raw) != nil {
				return
			}
			for _, r := range raw {
				parsed, err := epitech.Parse(org + "/" + r.Name)
				if err != nil {
					continue // not a project repo
				}
				all = append(all, Repo{Repo: parsed, SSHURL: r.SSHURL, URL: r.URL, CreatedAt: r.CreatedAt})
			}
		}(org)
	}
	wg.Wait()
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	if len(all) == 0 && first != nil {
		return nil, first
	}
	return all, nil
}

// CloneURL matches whatever protocol your existing clones use, so SSH users
// keep SSH and nobody gets surprise password prompts.
func (r Repo) CloneURL(preferSSH bool) string {
	if preferSSH && r.SSHURL != "" {
		return r.SSHURL
	}
	return r.URL + ".git"
}
