package awsctx

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListProfiles parses ~/.aws/config and ~/.aws/credentials and returns
// a de-duplicated, sorted list of profile names. Supports the standard
// section shapes:
//
//	[default]
//	[profile my-profile]   (config only)
//	[my-profile]           (credentials only)
//
// Unknown / malformed sections are skipped. Missing files are treated
// as empty — no error.
func ListProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	addFromConfig(filepath.Join(home, ".aws", "config"), true, seen)
	addFromConfig(filepath.Join(home, ".aws", "credentials"), false, seen)

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// addFromConfig reads one INI-ish file and adds every section name to
// `seen`. When isConfig is true, `profile ` prefixes are stripped so
// the config file's verbose form lines up with the credentials file's
// plain form.
func addFromConfig(path string, isConfig bool, seen map[string]struct{}) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		name := strings.TrimSpace(line[1 : len(line)-1])
		if isConfig && strings.HasPrefix(name, "profile ") {
			name = strings.TrimSpace(strings.TrimPrefix(name, "profile "))
		}
		if name == "" {
			continue
		}
		seen[name] = struct{}{}
	}
}

// CommonRegions is a curated list of AWS regions shown in the switcher
// overlay's region pane. Selected from the most commonly-used regions
// as of early 2026. Users whose profile resolves to a region outside
// this list still see that region pre-selected via the context's
// current Region value — the UI adds it to the list on the fly if it
// isn't already present.
var CommonRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"ca-central-1",
	"sa-east-1",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-central-1",
	"eu-north-1",
	"eu-south-1",
	"ap-south-1",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-east-1",
	"me-south-1",
	"af-south-1",
}
