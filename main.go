package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/cli/go-gh"
	"github.com/cli/go-gh/pkg/api"
	"github.com/cli/go-gh/pkg/browser"
	"github.com/cli/go-gh/pkg/jq"
	"github.com/cli/go-gh/pkg/jsonpretty"
	"github.com/cli/go-gh/pkg/repository"
	"github.com/cli/go-gh/pkg/tableprinter"
	"github.com/cli/go-gh/pkg/term"
)

// Flags holds the parsed flag values
type Flags struct {
	jsonFlag     bool
	jqFlag       string
	lucky        bool
	repoOverride string
	searchTerm   string
}

// Run the CLI
func runCLI() error {
	// Parse flags
	flags, err := parseFlags()
	if err != nil {
		return fmt.Errorf("failed to parse flags: %w", err)
	}

	// Determine repository
	repo, err := determineRepository(flags.repoOverride)
	if err != nil {
		return fmt.Errorf("could not determine repository: %w", err)
	}

	// Execute GraphQL query
	gqlClient, err := gh.GQLClient(nil)
	if err != nil {
		return fmt.Errorf("could not create a GraphQL client: %w", err)
	}
	response, err := executeGraphQLQuery(gqlClient, constructGraphQLQuery(repo))
	if err != nil {
		return fmt.Errorf("failed to talk to the GitHub API: %w", err)
	}

	// Handle discussions
	if !response.Repository.HasDiscussionsEnabled {
		return fmt.Errorf("%s/%s does not have discussions enabled", repo.Owner(), repo.Name())
	}
	matches := findMatchingDiscussions(response, flags.searchTerm)

	// No matches found
	if len(matches) == 0 {
		fmt.Fprintln(os.Stderr, "No matching discussion threads found :(")
		return nil
	}

	// Open the first matching result in a web browser if lucky flag is set
	if flags.lucky {
		b := browser.New("", os.Stdout, os.Stderr)
		return b.Browse(matches[0].URL)
	}

	// Check if output is JSON
	if flags.jsonFlag {
		return handleJSONOutput(matches, flags.jqFlag)
	}

	// Output in table format
	return outputInTableFormat(matches, repo, flags.searchTerm)
}

// Parse flags
func parseFlags() (Flags, error) {
	var flags Flags
	flag.BoolVar(&flags.jsonFlag, "json", false, "Output JSON")
	flag.StringVar(&flags.jqFlag, "jq", "", "Process JSON output with a jq expression")
	flag.BoolVar(&flags.lucky, "lucky", false, "Open the first matching result in a web browser")
	flag.StringVar(&flags.repoOverride, "repo", "", "Specify a repository. If omitted, uses current repository")
	flag.Parse()

	// Ensure search term provided
	if len(flag.Args()) < 1 {
		return flags, errors.New("search term required")
	}
	flags.searchTerm = strings.Join(flag.Args(), " ")

	return flags, nil
}

// Determine repository
func determineRepository(repoOverride string) (repository.Repository, error) {
	if repoOverride == "" {
		return gh.CurrentRepository()
	}
	return repository.Parse(repoOverride)
}

// Execute GraphQL query
func executeGraphQLQuery(client api.GQLClient, query string) (response struct {
	Repository struct {
		Discussions struct {
			Edges []struct {
				Node Discussion
			}
		}
		HasDiscussionsEnabled bool
	}
}, err error) {
	err = client.Do(query, nil, &response)
	return response, err
}

// Find matching discussions
func findMatchingDiscussions(response struct {
	Repository struct {
		Discussions           struct{ Edges []struct{ Node Discussion } }
		HasDiscussionsEnabled bool
	}
}, search string) []Discussion {
	matches := []Discussion{}
	for _, edge := range response.Repository.Discussions.Edges {
		if strings.Contains(edge.Node.Body+edge.Node.Title, search) {
			matches = append(matches, edge.Node)
		}
	}
	return matches
}

// Handle JSON output
func handleJSONOutput(matches []Discussion, jqFlag string) error {
	output, err := json.Marshal(matches)
	if err != nil {
		return fmt.Errorf("could not serialize JSON: %w", err)
	}
	if jqFlag != "" {
		return jq.Evaluate(bytes.NewBuffer(output), os.Stdout, jqFlag)
	}
	isTerminal := term.IsTerminal(os.Stdout)
	return jsonpretty.Format(os.Stdout, bytes.NewBuffer(output), " ", isTerminal)
}

// Output in table format
func outputInTableFormat(matches []Discussion, repo repository.Repository, search string) error {
	isTerminal := term.IsTerminal(os.Stdout)
	tp := tableprinter.New(os.Stdout, isTerminal, 100)

	if isTerminal {
		fmt.Printf(
			"Searching discussions in '%s/%s' for '%s'\n",
			repo.Owner(), repo.Name(), search)
	}

	fmt.Println()
	for _, d := range matches {
		tp.AddField(d.Title)
		tp.AddField(d.URL)
		tp.EndRow()
	}

	return tp.Render()
}

// Construct GraphQL query
func constructGraphQLQuery(repo repository.Repository) string {
	return fmt.Sprintf(`{
		repository(owner: "%s", name: "%s") {
			hasDiscussionsEnabled
			discussions(first: 100) {
				edges { node {
					title
					body
					url
	}}}}}`, repo.Owner(), repo.Name())
}

// Discussion struct represents a discussion on GitHub
type Discussion struct {
	Title string
	URL   string `json:"url"`
	Body  string
}

func main() {
	if err := runCLI(); err != nil {
		fmt.Fprintf(os.Stderr, "gh-ask failed: %s\n", err.Error())
		os.Exit(1)
	}
}
