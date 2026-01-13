package mappings

// GitHubToMattermost maps GitHub usernames to Mattermost usernames.
// Mattermost usernames will be prefixed with @ for mentions.
//
// To add a mapping:
//  1. Find the GitHub username (e.g., "john-doe")
//  2. Find their Mattermost username (e.g., "john.doe")
//  3. Add entry: "john-doe": "john.doe",
//
// Unmapped users will appear without @ mention and be listed in warnings.
var GitHubToMattermost = map[string]string{
	"Damanox":         "damanox",
	"IvanKenobe":      "aid",
	"a-porubai":       "praid92",
	"skynet2":         "applejack",
	"theAndEx":        "andrii",
	"HollaDollaHolla": "alex",
	"jenshen85":       "eugene_s",
}

var mattermostToGitHub map[string]string

func init() {
	mattermostToGitHub = make(map[string]string, len(GitHubToMattermost))
	for gh, mm := range GitHubToMattermost {
		mattermostToGitHub[mm] = gh
	}
}

func GitHubFromMattermost(mmUsername string) (string, bool) {
	gh, ok := mattermostToGitHub[mmUsername]
	return gh, ok
}

func MattermostFromGitHub(ghUsername string) (string, bool) {
	mm, ok := GitHubToMattermost[ghUsername]
	return mm, ok
}
