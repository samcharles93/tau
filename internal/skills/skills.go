package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	aimconfig "bitbucket.srv.westpac.com.au/m055731/aim/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	SkillFileName          = "SKILL.md"
	MaxNameLength          = 64
	MaxDescriptionLength   = 1024
	MaxCompatibilityLength = 500
	maxDiscoveryDepth      = 6

	userInteropPriority    = 10
	userLegacyPriority     = 20
	userNativePriority     = 30
	projectInteropPriority = 110
	projectClaudePriority  = 120
	projectNativePriority  = 130
	extraPathPriority      = 1000
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
	ScopeBuiltin Scope = "builtin"
)

type DiagnosticSeverity string

const (
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityError   DiagnosticSeverity = "error"
)

// Diagnostic reports a parse, validation, or precedence issue discovered while loading skills.
type Diagnostic struct {
	Path      string
	SkillName string
	Severity  DiagnosticSeverity
	Message   string
}

// Source describes one root directory to scan for skills.
type Source struct {
	Root     string
	Scope    Scope
	Priority int
}

// Skill represents a parsed skill directory.
type Skill struct {
	Name                   string            `yaml:"name" json:"name"`
	Description            string            `yaml:"description" json:"description"`
	UserInvocable          bool              `yaml:"user-invocable" json:"user_invocable"`
	DisableModelInvocation bool              `yaml:"disable-model-invocation" json:"disable_model_invocation"`
	License                string            `yaml:"license,omitempty" json:"license,omitempty"`
	Compatibility          string            `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	Metadata               map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	AllowedTools           string            `yaml:"allowed-tools,omitempty" json:"allowed_tools,omitempty"`
	Instructions           string            `yaml:"-" json:"instructions"`
	Path                   string            `yaml:"-" json:"path"`
	SkillFilePath          string            `yaml:"-" json:"skill_file_path"`
	Scope                  Scope             `yaml:"-" json:"scope"`
	SourceRoot             string            `yaml:"-" json:"source_root"`
	Priority               int               `yaml:"-" json:"priority"`
}

// UserSources returns the user-level skill roots AIM should scan.
func UserSources() []Source {
	sources := []Source{
		{
			Root:     filepath.Join(aimconfig.Dir(), "skills"),
			Scope:    ScopeUser,
			Priority: userNativePriority,
		},
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return sources
	}

	sources = append(sources,
		Source{Root: filepath.Join(homeDir, ".aim", "skills"), Scope: ScopeUser, Priority: userLegacyPriority},
		Source{Root: filepath.Join(homeDir, ".agents", "skills"), Scope: ScopeUser, Priority: userInteropPriority},
	)

	return sources
}

// ProjectSources returns the project-level skill roots AIM should scan.
func ProjectSources(workingDir string) []Source {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" {
		return nil
	}

	return []Source{
		{Root: filepath.Join(trimmed, ".agents", "skills"), Scope: ScopeProject, Priority: projectInteropPriority},
		{Root: filepath.Join(trimmed, ".claude", "skills"), Scope: ScopeProject, Priority: projectClaudePriority},
		{Root: filepath.Join(trimmed, ".aim", "skills"), Scope: ScopeProject, Priority: projectNativePriority},
	}
}

// DefaultSources returns the standard AIM skill discovery roots.
func DefaultSources(workingDir string) []Source {
	sources := append([]Source{}, UserSources()...)
	sources = append(sources, ProjectSources(workingDir)...)
	return sources
}

// Discover scans all sources, parses skills, applies precedence, and returns the surviving skills plus diagnostics.
func Discover(sources []Source) ([]*Skill, []Diagnostic) {
	var diagnostics []Diagnostic
	selected := make(map[string]*Skill)

	for _, source := range sources {
		root := strings.TrimSpace(source.Root)
		if root == "" {
			continue
		}

		info, err := os.Stat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: root, Severity: SeverityError, Message: err.Error()})
			continue
		}
		if !info.IsDir() {
			continue
		}

		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Severity: SeverityError, Message: walkErr.Error()})
				return nil
			}

			if entry.IsDir() {
				if shouldSkipDir(path, root, entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}

			if entry.Name() != SkillFileName {
				return nil
			}

			skill, parseDiagnostics, err := Parse(path)
			diagnostics = append(diagnostics, parseDiagnostics...)
			if err != nil || HasErrors(parseDiagnostics) {
				return nil
			}

			skill.Scope = source.Scope
			skill.SourceRoot = root
			skill.Priority = source.Priority

			key := strings.ToLower(skill.Name)
			if existing, ok := selected[key]; ok {
				if shouldReplaceSkill(existing, skill) {
					diagnostics = append(diagnostics, Diagnostic{
						Path:      existing.SkillFilePath,
						SkillName: existing.Name,
						Severity:  SeverityWarning,
						Message:   fmt.Sprintf("shadowed by %s", skill.SkillFilePath),
					})
					selected[key] = skill
				} else {
					diagnostics = append(diagnostics, Diagnostic{
						Path:      skill.SkillFilePath,
						SkillName: skill.Name,
						Severity:  SeverityWarning,
						Message:   fmt.Sprintf("shadowed by %s", existing.SkillFilePath),
					})
				}
				return nil
			}

			selected[key] = skill
			return nil
		})
		if walkErr != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: root, Severity: SeverityError, Message: walkErr.Error()})
		}
	}

	results := make([]*Skill, 0, len(selected))
	for _, skill := range selected {
		results = append(results, skill)
	}

	slices.SortFunc(results, func(left, right *Skill) int {
		if compare := strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)); compare != 0 {
			return compare
		}
		return strings.Compare(strings.ToLower(left.SkillFilePath), strings.ToLower(right.SkillFilePath))
	})

	slices.SortFunc(diagnostics, func(left, right Diagnostic) int {
		if compare := strings.Compare(strings.ToLower(left.Path), strings.ToLower(right.Path)); compare != 0 {
			return compare
		}
		return strings.Compare(strings.ToLower(left.Message), strings.ToLower(right.Message))
	})

	return results, diagnostics
}

// Parse reads and parses a single SKILL.md file.
func Parse(path string) (*Skill, []Diagnostic, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		diagnostic := Diagnostic{Path: path, Severity: SeverityError, Message: err.Error()}
		return nil, []Diagnostic{diagnostic}, err
	}

	frontmatter, body, err := splitFrontmatter(string(content))
	if err != nil {
		diagnostic := Diagnostic{Path: path, Severity: SeverityError, Message: err.Error()}
		return nil, []Diagnostic{diagnostic}, err
	}

	var skill Skill
	if err := unmarshalFrontmatter(frontmatter, &skill); err != nil {
		diagnostic := Diagnostic{Path: path, Severity: SeverityError, Message: fmt.Sprintf("parsing frontmatter: %v", err)}
		return nil, []Diagnostic{diagnostic}, err
	}

	skill.Path = filepath.Dir(path)
	skill.SkillFilePath = path
	skill.Instructions = strings.TrimSpace(body)

	diagnostics := validateSkill(&skill)
	if HasErrors(diagnostics) {
		return nil, diagnostics, errors.New("skill validation failed")
	}

	return &skill, diagnostics, nil
}

// FilterDisabled returns skills not listed in disabledNames.
func FilterDisabled(skillSet []*Skill, disabledNames []string) []*Skill {
	if len(disabledNames) == 0 {
		return cloneSkills(skillSet)
	}

	disabled := make(map[string]struct{}, len(disabledNames))
	for _, name := range disabledNames {
		trimmed := strings.ToLower(strings.TrimSpace(name))
		if trimmed == "" {
			continue
		}
		disabled[trimmed] = struct{}{}
	}

	filtered := make([]*Skill, 0, len(skillSet))
	for _, skill := range skillSet {
		if skill == nil {
			continue
		}
		if _, blocked := disabled[strings.ToLower(skill.Name)]; blocked {
			continue
		}
		filtered = append(filtered, cloneSkill(skill))
	}

	return filtered
}

// FilterUserInvocable returns only user-invocable skills.
func FilterUserInvocable(skillSet []*Skill) []*Skill {
	filtered := make([]*Skill, 0, len(skillSet))
	for _, skill := range skillSet {
		if skill == nil || !skill.UserInvocable {
			continue
		}
		filtered = append(filtered, cloneSkill(skill))
	}
	return filtered
}

// HasErrors reports whether any diagnostic is fatal.
func HasErrors(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// ToPromptXML renders the tier-1 skill catalog for prompt injection.
func ToPromptXML(skillSet []*Skill) string {
	if len(skillSet) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("<available_skills>\n")
	for _, skill := range skillSet {
		if skill == nil || skill.DisableModelInvocation {
			continue
		}
		builder.WriteString("  <skill>\n")
		fmt.Fprintf(&builder, "    <name>%s</name>\n", escapeXML(skill.Name))
		fmt.Fprintf(&builder, "    <description>%s</description>\n", escapeXML(skill.Description))
		fmt.Fprintf(&builder, "    <location>%s</location>\n", escapeXML(skill.SkillFilePath))
		builder.WriteString("  </skill>\n")
	}
	builder.WriteString("</available_skills>")
	return builder.String()
}

func AdditionalSources(paths []string, scope Scope) []Source {
	sources := make([]Source, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		sources = append(sources, Source{Root: trimmed, Scope: scope, Priority: extraPathPriority})
	}
	return sources
}

func validateSkill(skill *Skill) []Diagnostic {
	var diagnostics []Diagnostic
	name := strings.TrimSpace(skill.Name)
	if name == "" {
		diagnostics = append(diagnostics, Diagnostic{Path: skill.SkillFilePath, Severity: SeverityError, Message: "name is required"})
	} else {
		if len(name) > MaxNameLength {
			diagnostics = append(diagnostics, Diagnostic{Path: skill.SkillFilePath, SkillName: name, Severity: SeverityWarning, Message: fmt.Sprintf("name exceeds %d characters", MaxNameLength)})
		}
		if !skillNamePattern.MatchString(name) {
			diagnostics = append(diagnostics, Diagnostic{Path: skill.SkillFilePath, SkillName: name, Severity: SeverityWarning, Message: "name should contain lowercase letters, numbers, and single hyphens only"})
		}
		parentDir := filepath.Base(filepath.Dir(skill.SkillFilePath))
		if parentDir != "" && parentDir != "." && parentDir != name {
			diagnostics = append(diagnostics, Diagnostic{Path: skill.SkillFilePath, SkillName: name, Severity: SeverityWarning, Message: fmt.Sprintf("name %q does not match parent directory %q", name, parentDir)})
		}
	}

	description := strings.TrimSpace(skill.Description)
	if description == "" {
		diagnostics = append(diagnostics, Diagnostic{Path: skill.SkillFilePath, SkillName: name, Severity: SeverityError, Message: "description is required"})
	} else if len(description) > MaxDescriptionLength {
		diagnostics = append(diagnostics, Diagnostic{Path: skill.SkillFilePath, SkillName: name, Severity: SeverityWarning, Message: fmt.Sprintf("description exceeds %d characters", MaxDescriptionLength)})
	}

	compatibility := strings.TrimSpace(skill.Compatibility)
	if compatibility != "" && len(compatibility) > MaxCompatibilityLength {
		diagnostics = append(diagnostics, Diagnostic{Path: skill.SkillFilePath, SkillName: name, Severity: SeverityWarning, Message: fmt.Sprintf("compatibility exceeds %d characters", MaxCompatibilityLength)})
	}

	return diagnostics
}

func splitFrontmatter(content string) (string, string, error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	lines := strings.Split(content, "\n")
	start := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		start = index
		break
	}
	if start == -1 || strings.TrimSpace(lines[start]) != "---" {
		return "", "", errors.New("no YAML frontmatter found")
	}

	end := -1
	for index := start + 1; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "---" {
			end = index
			break
		}
	}
	if end == -1 {
		return "", "", errors.New("unclosed frontmatter")
	}

	frontmatter := strings.Join(lines[start+1:end], "\n")
	body := strings.Join(lines[end+1:], "\n")
	return frontmatter, body, nil
}

func unmarshalFrontmatter(frontmatter string, skill *Skill) error {
	if err := yaml.Unmarshal([]byte(frontmatter), skill); err == nil {
		return nil
	}

	normalized := quoteColonValues(frontmatter)
	if normalized == frontmatter {
		return yaml.Unmarshal([]byte(frontmatter), skill)
	}
	if err := yaml.Unmarshal([]byte(normalized), skill); err != nil {
		return err
	}
	return nil
}

func quoteColonValues(frontmatter string) string {
	lines := strings.Split(frontmatter, "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		separator := strings.Index(line, ":")
		if separator <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if value == "" || value == "|" || value == ">" {
			continue
		}
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			continue
		}
		if !strings.Contains(value, ":") {
			continue
		}
		if key == "metadata" {
			continue
		}

		escaped := strings.ReplaceAll(value, "\"", "\\\"")
		lines[index] = line[:separator+1] + " \"" + escaped + "\""
	}
	return strings.Join(lines, "\n")
}

func shouldSkipDir(path, root, name string) bool {
	if name == ".git" || name == "node_modules" {
		return true
	}

	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if relative == "." {
		return false
	}
	depth := strings.Count(filepath.ToSlash(relative), "/") + 1
	return depth > maxDiscoveryDepth
}

func shouldReplaceSkill(existing, candidate *Skill) bool {
	if candidate.Priority != existing.Priority {
		return candidate.Priority > existing.Priority
	}
	return strings.Compare(strings.ToLower(candidate.SkillFilePath), strings.ToLower(existing.SkillFilePath)) > 0
}

func cloneSkill(skill *Skill) *Skill {
	if skill == nil {
		return nil
	}
	clone := *skill
	if skill.Metadata != nil {
		clone.Metadata = make(map[string]string, len(skill.Metadata))
		maps.Copy(clone.Metadata, skill.Metadata)
	}
	return &clone
}

func cloneSkills(skillSet []*Skill) []*Skill {
	if skillSet == nil {
		return nil
	}
	clones := make([]*Skill, 0, len(skillSet))
	for _, skill := range skillSet {
		clones = append(clones, cloneSkill(skill))
	}
	return clones
}

func cloneDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	if diagnostics == nil {
		return nil
	}
	clones := make([]Diagnostic, len(diagnostics))
	copy(clones, diagnostics)
	return clones
}

func escapeXML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}
