package commands

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	aimconfig "bitbucket.srv.westpac.com.au/m055731/aim/internal/config"
	"bitbucket.srv.westpac.com.au/m055731/aim/internal/skills"
)

var namedArgPattern = regexp.MustCompile(`\$([A-Z][A-Z0-9_]*)`)

const (
	userCommandPrefix    = "user:"
	projectCommandPrefix = "project:"
)

// Argument represents a command argument with its metadata.
type Argument struct {
	ID          string
	Title       string
	Description string
	Required    bool
}

// CustomCommand represents a user-defined custom command loaded from markdown files.
type CustomCommand struct {
	ID        string
	Name      string
	Content   string
	Arguments []Argument
	Skill     *skills.Skill
}

type commandSource struct {
	path   string
	prefix string
}

// LoadCustomCommands loads custom commands from user and project directories.
func LoadCustomCommands(workingDir string) ([]CustomCommand, error) {
	return loadAll(buildCommandSources(workingDir))
}

// LoadSkillCommands loads user-level user-invocable skills as commands.
func LoadSkillCommands() []CustomCommand {
	skillSet, _ := skills.Discover(skills.UserSources())
	return commandsFromSkills(skills.FilterUserInvocable(skillSet), userCommandPrefix)
}

// LoadProjectSkillCommands loads project-level user-invocable skills as commands.
func LoadProjectSkillCommands(workingDir string) []CustomCommand {
	skillSet, _ := skills.Discover(skills.ProjectSources(workingDir))
	return commandsFromSkills(skills.FilterUserInvocable(skillSet), projectCommandPrefix)
}

func buildCommandSources(workingDir string) []commandSource {
	sources := []commandSource{
		{
			path:   filepath.Join(aimconfig.Dir(), "commands"),
			prefix: userCommandPrefix,
		},
	}

	if legacyDir := legacyHomeCommandsDir(); legacyDir != "" {
		sources = append(sources, commandSource{path: legacyDir, prefix: userCommandPrefix})
	}
	if projectDir := projectCommandsDir(workingDir); projectDir != "" {
		sources = append(sources, commandSource{path: projectDir, prefix: projectCommandPrefix})
	}

	return sources
}

func legacyHomeCommandsDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".aim", "commands")
}

func projectCommandsDir(workingDir string) string {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" {
		return ""
	}
	return filepath.Join(trimmed, ".aim", "commands")
}

func loadAll(sources []commandSource) ([]CustomCommand, error) {
	var commands []CustomCommand

	for _, source := range sources {
		if cmds, err := loadFromSource(source); err == nil {
			commands = append(commands, cmds...)
		}
	}

	return commands, nil
}

func loadFromSource(source commandSource) ([]CustomCommand, error) {
	if _, err := os.Stat(source.path); os.IsNotExist(err) {
		return nil, nil
	}

	var commands []CustomCommand

	err := filepath.WalkDir(source.path, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isMarkdownFile(d.Name()) {
			return err
		}

		cmd, err := loadCommand(path, source.path, source.prefix)
		if err != nil {
			return nil // Skip invalid files
		}

		commands = append(commands, cmd)
		return nil
	})

	return commands, err
}

func loadCommand(path, baseDir, prefix string) (CustomCommand, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return CustomCommand{}, err
	}

	id := buildCommandID(path, baseDir, prefix)

	return CustomCommand{
		ID:        id,
		Name:      id,
		Content:   string(content),
		Arguments: extractArgNames(string(content)),
	}, nil
}

func extractArgNames(content string) []Argument {
	matches := namedArgPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var args []Argument

	for _, match := range matches {
		arg := match[1]
		if !seen[arg] {
			seen[arg] = true
			// for normal custom commands, all args are required
			args = append(args, Argument{ID: arg, Title: arg, Required: true})
		}
	}

	return args
}

func buildCommandID(path, baseDir, prefix string) string {
	relPath, _ := filepath.Rel(baseDir, path)
	parts := strings.Split(relPath, string(filepath.Separator))

	// Remove .md extension from last part
	if len(parts) > 0 {
		lastIdx := len(parts) - 1
		parts[lastIdx] = strings.TrimSuffix(parts[lastIdx], filepath.Ext(parts[lastIdx]))
	}

	return prefix + strings.Join(parts, ":")
}

func isMarkdownFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".md")
}

func commandsFromSkills(skillSet []*skills.Skill, prefix string) []CustomCommand {
	commands := make([]CustomCommand, 0, len(skillSet))
	for _, skill := range skillSet {
		if skill == nil {
			continue
		}
		name := prefix + skill.Name
		commands = append(commands, CustomCommand{
			ID:        name,
			Name:      name,
			Content:   skill.Instructions,
			Arguments: extractArgNames(skill.Instructions),
			Skill:     skill,
		})
	}
	return commands
}
