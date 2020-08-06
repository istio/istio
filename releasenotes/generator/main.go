package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/russross/blackfriday"
)

type ReleaseNote struct {
	Kind          string      `json:"kind"`
	Area          string      `json:"area"`
	Issue         []string    `json:"issue",omitempty`
	ReleaseNotes  string      `json:"releaseNotes"`
	UpgradeNotes  upgradeNote `json:"upgradeNotes"`
	SecurityNotes string      `json:"securityNotes"`
}

type upgradeNote struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func main() {
	files, err := getFilesWithExtension("notes", "yaml")
	if err != nil {
		fmt.Printf("failed to list files: %s", err.Error())
		return
	}
	fmt.Printf("Release notes files: %+v\n\n", files)

	releaseNotes, err := parseReleaseNotesFiles("notes", files)
	if err != nil {
		fmt.Printf("Unable to read release notes: %s\n", err.Error())
	}

	files, err = getFilesWithExtension("templates", "md")
	if err != nil {
		fmt.Printf("failed to list files: %s", err.Error())
		return
	}

	fmt.Printf("Markdown templates: %+v\n\n", files)
	for _, file := range files {
		output, err := parseTemplate("templates", file, releaseNotes)
		if err != nil {
			fmt.Printf("Failed to parse markdown template: %s", err.Error())
			return
		}

		ioutil.WriteFile(file, []byte(output), 0644)
		fmt.Printf("Wrote markdown to %s\n", file)
		ioutil.WriteFile(file+".html", []byte(markdownToHTML(output)), 0644)
		fmt.Printf("Wrote markdown to %s.html\n", file)
	}
}

//Bavery_todo: issue display formatting template
//Bavery_todo: flag for out to html or markdown
//Bavery_TODO: find previous branch
//Bavery_todo: diff previous branch
//bavery_todo: complete format parsing and make it generic (i.e. no intelligence of release notes vs upgrade notes vs security notes). Do we need to know about area in here?

func parseReleaseNote(releaseNotes []ReleaseNote, format string) ([]string, error) {
	parsedNotes := make([]string, 0, 0)

	noteType := "releaseNotes"
	if strings.Contains(format, "upgradeNotes") {
		noteType = "upgradeNotes"
	} else if strings.Contains(format, "securityNotes") {
		noteType = "securityNotes"
	}

	area := ""
	areaRegexp := regexp.MustCompile("area:[a-zA-Z-]*")
	if match := areaRegexp.FindString(format); match != "" {
		sections := strings.Split(match, ":")
		area = sections[1]
	}
	fmt.Printf("Notes format: %s type: %s area: %s\n", format, noteType, area)

	for _, note := range releaseNotes {
		formatted := ""
		if noteType == "releaseNotes" && note.ReleaseNotes != "" && (note.Area == area || area == "") {
			formatted = fmt.Sprintf("- %s %s", note.ReleaseNotes, issuesListToString(note.Issue))
		} else if noteType == "upgradeNotes" && note.UpgradeNotes.Content != "" {
			if note.UpgradeNotes.Title == "" {
				fmt.Printf("Upgrade note is missing title. Skipping.")
			} else {
				formatted = fmt.Sprintf("## %s\n%s", note.UpgradeNotes.Title, note.UpgradeNotes.Content)
			}
		} else if noteType == "securityNotes" && note.SecurityNotes != "" {
			formatted = fmt.Sprintf("- %s", note.SecurityNotes)
		}

		if formatted != "" {
			parsedNotes = append(parsedNotes, formatted)
		}
	}

	return parsedNotes, nil
}

//Bavery_TODO: rewrite
func issuesListToString(issues []string) string {
	issueString := ""
	for _, issue := range issues {
		if issueString != "" {
			issueString += ","
		}
		if strings.Contains(issue, "github.com") {
			issueNumber := path.Base(issue)
			issueString += fmt.Sprintf("([Issue #%s](%s))", issueNumber, issue)
		} else {
			issueString += fmt.Sprintf("([Issue #%s](https://github.com/istio/istio/issues/%s))", issue, issue)
		}
	}
	return issueString
}

//getFilesWithExtension returns the files from filePath with extension extension
func getFilesWithExtension(filePath string, extension string) ([]string, error) {
	directory, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open directory: %s", err.Error())
	}
	defer directory.Close()

	var files []string
	files, err = directory.Readdirnames(0)
	if err != nil {
		return nil, fmt.Errorf("unable to list files for directory %s: %s", filePath, err.Error())
	}

	filesWithExtension := make([]string, 0, 0)
	for _, fileName := range files {
		if strings.HasSuffix(fileName, extension) {
			filesWithExtension = append(filesWithExtension, fileName)
		}
	}

	return filesWithExtension, nil

}

func parseReleaseNotesFiles(filePath string, files []string) ([]ReleaseNote, error) {
	releaseNotes := make([]ReleaseNote, 0, 0)
	for _, file := range files {
		file = path.Join(filePath, file)
		contents, err := ioutil.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("unable to open file %s: %s", file, err.Error())
		}

		var releaseNote ReleaseNote
		if err = yaml.Unmarshal(contents, &releaseNote); err != nil {
			return nil, fmt.Errorf("Unable to parse release note %s:%s", file, err.Error())
		}
		releaseNotes = append(releaseNotes, releaseNote)

	}
	return releaseNotes, nil

}

//markdownToHTML is a wrapper around the blackfriday library to generate HTML previews from markdown
func markdownToHTML(markdown string) string {
	return string(blackfriday.MarkdownCommon([]byte(markdown)))
}

func parseTemplate(filepath string, filename string, releaseNotes []ReleaseNote) (string, error) {
	filename = path.Join(filepath, filename)
	fmt.Printf("Processing %s\n", filename)

	contents, err := ioutil.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("unable to open file %s: %s", filename, err.Error())
	}

	comment := regexp.MustCompile("<!--(.*)-->")
	output := string(contents)
	results := comment.FindAllString(string(contents), -1)

	for _, result := range results {
		contents, err := parseReleaseNote(releaseNotes, result)
		if err != nil {
			fmt.Printf("Could not generate content: %s", err.Error())
			return "", fmt.Errorf("could not generate content: %s", err.Error())
		}
		joinedContents := strings.Join(contents, "\n")
		output = strings.Replace(output, result, joinedContents, -1)
	}

	return output, nil
}
