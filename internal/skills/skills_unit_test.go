package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// requireDiagnostics asserts the exact set of error and warning messages in
// diags, regardless of order.
func requireDiagnostics(t *testing.T, diags []Diagnostic, wantErrs, wantWarns []string) {
	t.Helper()
	var errs, warns []string
	for _, diag := range diags {
		switch diag.Severity {
		case SeverityError:
			errs = append(errs, diag.Message)
		case SeverityWarning:
			warns = append(warns, diag.Message)
		}
	}
	require.ElementsMatch(t, wantErrs, errs)
	require.ElementsMatch(t, wantWarns, warns)
}

func TestSplitFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantFront string
		wantBody  string
		wantErr   string
	}{
		{
			name:      "simple frontmatter and body",
			content:   "---\nname: a\ndescription: b\n---\n\n# Hello\n",
			wantFront: "name: a\ndescription: b",
			wantBody:  "\n# Hello\n",
		},
		{
			name:    "empty content has no frontmatter",
			content: "",
			wantErr: "no YAML frontmatter found",
		},
		{
			name:    "whitespace only content has no frontmatter",
			content: "   \n\t\n  \n",
			wantErr: "no YAML frontmatter found",
		},
		{
			name:    "markdown without frontmatter errors",
			content: "# just markdown\n",
			wantErr: "no YAML frontmatter found",
		},
		{
			name:    "first non-blank line must be the delimiter",
			content: "name: a\n---\nbody",
			wantErr: "no YAML frontmatter found",
		},
		{
			name:    "unclosed frontmatter errors",
			content: "---\nname: a\n",
			wantErr: "unclosed frontmatter",
		},
		{
			name:      "BOM prefix is stripped",
			content:   "\uFEFF---\nname: a\n---\nbody",
			wantFront: "name: a",
			wantBody:  "body",
		},
		{
			name:      "CRLF line endings are normalized",
			content:   "---\r\nname: a\r\ndescription: b\r\n---\r\nbody\r\nline2",
			wantFront: "name: a\ndescription: b",
			wantBody:  "body\nline2",
		},
		{
			name:      "lone CR line endings are normalized",
			content:   "---\rname: a\r---\rbody",
			wantFront: "name: a",
			wantBody:  "body",
		},
		{
			name:      "leading blank lines before opening delimiter",
			content:   "\n\n  ---\nname: a\n---\nbody",
			wantFront: "name: a",
			wantBody:  "body",
		},
		{
			name:      "whitespace around closing delimiter is tolerated",
			content:   "---\nname: a\n  ---  \nbody",
			wantFront: "name: a",
			wantBody:  "body",
		},
		{
			name:      "empty body",
			content:   "---\nname: a\n---",
			wantFront: "name: a",
			wantBody:  "",
		},
		{
			name:      "body may contain delimiter-looking lines",
			content:   "---\nname: a\n---\n---\ntext",
			wantFront: "name: a",
			wantBody:  "---\ntext",
		},
		{
			name:      "blank lines inside frontmatter are preserved",
			content:   "---\nname: a\n\ndescription: b\n---\nbody",
			wantFront: "name: a\n\ndescription: b",
			wantBody:  "body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			frontmatter, body, err := SplitFrontmatter(tt.content)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				require.Empty(t, frontmatter)
				require.Empty(t, body)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantFront, frontmatter)
			require.Equal(t, tt.wantBody, body)
		})
	}
}

func TestUnmarshalFrontmatter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		frontmatter string
		want        Skill
		wantErr     bool
	}{
		{
			name:        "plain scalars parse directly",
			frontmatter: "name: a\ndescription: b\ncompatibility: tau",
			want:        Skill{Name: "a", Description: "b", Compatibility: "tau"},
		},
		{
			name:        "unquoted colon value triggers quoting retry",
			frontmatter: "name: a\ndescription: Handles X: Y",
			want:        Skill{Name: "a", Description: "Handles X: Y"},
		},
		{
			name:        "already double-quoted colon value",
			frontmatter: "name: a\ndescription: \"a: b\"",
			want:        Skill{Name: "a", Description: "a: b"},
		},
		{
			name:        "single-quoted colon value",
			frontmatter: "name: a\ndescription: 'a: b'",
			want:        Skill{Name: "a", Description: "a: b"},
		},
		{
			name:        "literal block scalar keeps colons as content",
			frontmatter: "name: a\ndescription: |\n  line: one\n  line two\n",
			want:        Skill{Name: "a", Description: "line: one\nline two\n"},
		},
		{
			name:        "folded block scalar with url",
			frontmatter: "name: a\ndescription: >\n  see http://example.com:8080/x\n  next line\n",
			want:        Skill{Name: "a", Description: "see http://example.com:8080/x next line\n"},
		},
		{
			name:        "nested metadata mapping",
			frontmatter: "name: a\nmetadata:\n  author: sam\n  note: hello",
			want:        Skill{Name: "a", Metadata: map[string]string{"author": "sam", "note": "hello"}},
		},
		{
			name:        "boolean fields",
			frontmatter: "name: a\nuser-invocable: true\ndisable-model-invocation: true\nenabled: false",
			want:        Skill{Name: "a", UserInvocable: true, DisableModelInvocation: true, Enabled: false},
		},
		{
			name:        "unfixable yaml returns an error",
			frontmatter: "name: a\n  bad: indent",
			wantErr:     true,
		},
		{
			name:        "inline metadata value with colon still errors",
			frontmatter: "metadata: a: b",
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var skill Skill
			err := UnmarshalFrontmatter(tt.frontmatter, &skill)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, skill)
		})
	}

	t.Run("unmarshals into arbitrary targets", func(t *testing.T) {
		t.Parallel()
		var generic map[string]any
		require.NoError(t, UnmarshalFrontmatter("key: value", &generic))
		require.Equal(t, map[string]any{"key": "value"}, generic)
	})
}

func TestQuoteColonValuesEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "block scalar header with trailing comment",
			in:   "description: | # note\n  a: b\nother: x: y",
			want: "description: | # note\n  a: b\nother: \"x: y\"",
		},
		{
			name: "nested mapping value is quoted",
			in:   "metadata:\n  note: v: x",
			want: "metadata:\n  note: \"v: x\"",
		},
		{
			name: "sequence item mapping value with colon is quoted",
			in:   "examples:\n  - a: b: c\nother: x",
			want: "examples:\n  - a: \"b: c\"\nother: x",
		},
		{
			name: "value that is only a colon is quoted",
			in:   "key: :",
			want: "key: \":\"",
		},
		{
			name: "top-level block scalar followed by sibling key",
			in:   "description: |\n  a: b\nname: x: y",
			want: "description: |\n  a: b\nname: \"x: y\"",
		},
		{
			name: "empty frontmatter stays empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, quoteColonValues(tt.in))
		})
	}
}

func TestValidateSkill(t *testing.T) {
	t.Parallel()

	longName := strings.Repeat("a", MaxNameLength+1)
	pathFor := func(dir string) string {
		return filepath.Join("skills", dir, "SKILL.md")
	}

	tests := []struct {
		name      string
		skill     Skill
		wantErrs  []string
		wantWarns []string
	}{
		{
			name:  "valid skill has no diagnostics",
			skill: Skill{Name: "my-skill", Description: "Does things", SkillFilePath: pathFor("my-skill"), Instructions: "body"},
		},
		{
			name:  "skill file at working root skips parent dir check",
			skill: Skill{Name: "a", Description: "d", SkillFilePath: "SKILL.md"},
		},
		{
			name:     "missing name is an error",
			skill:    Skill{Description: "d", SkillFilePath: pathFor("x")},
			wantErrs: []string{"name is required"},
		},
		{
			name:     "blank name is an error",
			skill:    Skill{Name: "   ", Description: "d", SkillFilePath: pathFor("x")},
			wantErrs: []string{"name is required"},
		},
		{
			name:      "name too long warns",
			skill:     Skill{Name: longName, Description: "d", SkillFilePath: pathFor(longName)},
			wantWarns: []string{fmt.Sprintf("name exceeds %d characters", MaxNameLength)},
		},
		{
			name:      "name with invalid characters warns",
			skill:     Skill{Name: "My_Skill", Description: "d", SkillFilePath: pathFor("My_Skill")},
			wantWarns: []string{"name should contain lowercase letters, numbers, and single hyphens only"},
		},
		{
			name:      "name directory mismatch warns",
			skill:     Skill{Name: "alpha", Description: "d", SkillFilePath: pathFor("beta")},
			wantWarns: []string{fmt.Sprintf("name %q does not match parent directory %q", "alpha", "beta")},
		},
		{
			name:     "missing description is an error",
			skill:    Skill{Name: "alpha", SkillFilePath: pathFor("alpha")},
			wantErrs: []string{"description is required"},
		},
		{
			name:      "description too long warns",
			skill:     Skill{Name: "alpha", Description: strings.Repeat("d", MaxDescriptionLength+1), SkillFilePath: pathFor("alpha")},
			wantWarns: []string{fmt.Sprintf("description exceeds %d characters", MaxDescriptionLength)},
		},
		{
			name:      "compatibility too long warns",
			skill:     Skill{Name: "alpha", Description: "d", Compatibility: strings.Repeat("c", MaxCompatibilityLength+1), SkillFilePath: pathFor("alpha")},
			wantWarns: []string{fmt.Sprintf("compatibility exceeds %d characters", MaxCompatibilityLength)},
		},
		{
			name:     "instructions too long is an error",
			skill:    Skill{Name: "alpha", Description: "d", Instructions: strings.Repeat("i", MaxInstructionsLength+1), SkillFilePath: pathFor("alpha")},
			wantErrs: []string{fmt.Sprintf("instructions exceed maximum length of %d characters", MaxInstructionsLength)},
		},
		{
			name:     "missing name and description produce two errors",
			skill:    Skill{SkillFilePath: pathFor("x")},
			wantErrs: []string{"name is required", "description is required"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diags := validateSkill(&tt.skill)
			requireDiagnostics(t, diags, tt.wantErrs, tt.wantWarns)
			for _, diag := range diags {
				require.Equal(t, tt.skill.SkillFilePath, diag.Path)
			}
		})
	}
}

func TestValidateSkillSetsSkillNameOnDiagnostics(t *testing.T) {
	t.Parallel()

	skill := Skill{Name: "alpha", Description: "d", SkillFilePath: filepath.Join("skills", "beta", "SKILL.md")}
	diags := validateSkill(&skill)
	require.Len(t, diags, 1)
	require.Equal(t, "alpha", diags[0].SkillName)
	require.Equal(t, SeverityWarning, diags[0].Severity)
	require.Equal(t, skill.SkillFilePath, diags[0].Path)
}

func TestFilterDisabled(t *testing.T) {
	t.Parallel()

	newSkill := func(name string, enabled bool) *Skill {
		return &Skill{Name: name, Enabled: enabled, Metadata: map[string]string{"name": name}}
	}

	t.Run("nil set with nil disabled list returns nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, FilterDisabled(nil, nil))
	})

	t.Run("nil set with disabled names returns empty", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, FilterDisabled(nil, []string{"x"}))
	})

	t.Run("empty disabled list returns clones of everything", func(t *testing.T) {
		t.Parallel()
		in := []*Skill{newSkill("a", true), nil, newSkill("b", true)}
		out := FilterDisabled(in, []string{})
		require.Len(t, out, 3)
		require.Equal(t, "a", out[0].Name)
		require.Nil(t, out[1])
		require.Equal(t, "b", out[2].Name)
		require.NotSame(t, in[0], out[0])
		require.NotSame(t, in[2], out[2])
	})

	t.Run("disabled names match case insensitively and blanks are ignored", func(t *testing.T) {
		t.Parallel()
		in := []*Skill{newSkill("Alpha", true), newSkill("beta", true), newSkill("gamma", true)}
		out := FilterDisabled(in, []string{" ALPHA ", "", "   ", "BETA"})
		require.Len(t, out, 1)
		require.Equal(t, "gamma", out[0].Name)
	})

	t.Run("enabled false is filtered out", func(t *testing.T) {
		t.Parallel()
		in := []*Skill{newSkill("a", true), newSkill("b", false)}
		out := FilterDisabled(in, []string{"zzz"})
		require.Len(t, out, 1)
		require.Equal(t, "a", out[0].Name)
	})

	t.Run("nil entries are skipped", func(t *testing.T) {
		t.Parallel()
		in := []*Skill{nil, newSkill("a", true)}
		out := FilterDisabled(in, []string{"zzz"})
		require.Len(t, out, 1)
		require.Equal(t, "a", out[0].Name)
	})

	t.Run("results are deep clones", func(t *testing.T) {
		t.Parallel()
		in := []*Skill{newSkill("a", true)}
		out := FilterDisabled(in, []string{"zzz"})
		require.Len(t, out, 1)
		out[0].Name = "changed"
		out[0].Metadata["name"] = "changed"
		require.Equal(t, "a", in[0].Name)
		require.Equal(t, "a", in[0].Metadata["name"])
	})
}

func TestFilterUserInvocable(t *testing.T) {
	t.Parallel()

	t.Run("empty input returns empty", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, FilterUserInvocable(nil))
		require.Empty(t, FilterUserInvocable([]*Skill{}))
	})

	t.Run("keeps only user invocable skills and skips nil", func(t *testing.T) {
		t.Parallel()
		invocable := &Skill{Name: "slash", UserInvocable: true}
		in := []*Skill{nil, {Name: "hidden"}, invocable, {Name: "other"}}
		out := FilterUserInvocable(in)
		require.Len(t, out, 1)
		require.Equal(t, "slash", out[0].Name)
		require.NotSame(t, invocable, out[0])
	})

	t.Run("results are deep clones", func(t *testing.T) {
		t.Parallel()
		in := []*Skill{{Name: "slash", UserInvocable: true, Metadata: map[string]string{"k": "v"}}}
		out := FilterUserInvocable(in)
		require.Len(t, out, 1)
		out[0].Metadata["k"] = "changed"
		require.Equal(t, "v", in[0].Metadata["k"])
	})
}

func TestShouldReplaceSkill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		existing  *Skill
		candidate *Skill
		want      bool
	}{
		{
			name:      "higher priority replaces",
			existing:  &Skill{Priority: 10, SkillFilePath: "/a/SKILL.md"},
			candidate: &Skill{Priority: 20, SkillFilePath: "/b/SKILL.md"},
			want:      true,
		},
		{
			name:      "lower priority does not replace",
			existing:  &Skill{Priority: 20, SkillFilePath: "/a/SKILL.md"},
			candidate: &Skill{Priority: 10, SkillFilePath: "/b/SKILL.md"},
			want:      false,
		},
		{
			name:      "same priority lexicographically greater path wins",
			existing:  &Skill{Priority: 10, SkillFilePath: "/a/b/SKILL.md"},
			candidate: &Skill{Priority: 10, SkillFilePath: "/a/c/SKILL.md"},
			want:      true,
		},
		{
			name:      "same priority lexicographically smaller path loses",
			existing:  &Skill{Priority: 10, SkillFilePath: "/a/c/SKILL.md"},
			candidate: &Skill{Priority: 10, SkillFilePath: "/a/b/SKILL.md"},
			want:      false,
		},
		{
			name:      "same priority identical path loses",
			existing:  &Skill{Priority: 10, SkillFilePath: "/a/b/SKILL.md"},
			candidate: &Skill{Priority: 10, SkillFilePath: "/a/b/SKILL.md"},
			want:      false,
		},
		{
			name:      "path tiebreak is case insensitive",
			existing:  &Skill{Priority: 10, SkillFilePath: "/A/B/SKILL.md"},
			candidate: &Skill{Priority: 10, SkillFilePath: "/a/c/SKILL.md"},
			want:      true,
		},
		{
			name:      "path tiebreak case insensitive reverse",
			existing:  &Skill{Priority: 10, SkillFilePath: "/a/C/SKILL.md"},
			candidate: &Skill{Priority: 10, SkillFilePath: "/A/b/SKILL.md"},
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, shouldReplaceSkill(tt.existing, tt.candidate))
		})
	}
}

func TestShouldSkipDir(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "root")
	deepPath := func(depth int) string {
		path := root
		for range depth {
			path = filepath.Join(path, "d")
		}
		return path
	}

	tests := []struct {
		name    string
		path    string
		dirName string
		want    bool
	}{
		{name: "git dir is skipped", path: filepath.Join(root, ".git"), dirName: ".git", want: true},
		{name: "node_modules is skipped", path: filepath.Join(root, "node_modules"), dirName: "node_modules", want: true},
		{name: "root itself is not skipped", path: root, dirName: filepath.Base(root), want: false},
		{name: "shallow dir is not skipped", path: filepath.Join(root, "skill"), dirName: "skill", want: false},
		{name: "depth at the limit is not skipped", path: deepPath(maxDiscoveryDepth), dirName: "d", want: false},
		{name: "depth beyond the limit is skipped", path: deepPath(maxDiscoveryDepth + 1), dirName: "d", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, shouldSkipDir(tt.path, root, tt.dirName))
		})
	}
}

func TestEscapeXML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "plain text", want: "plain text"},
		{in: "&", want: "&amp;"},
		{in: "<tag>", want: "&lt;tag&gt;"},
		{in: `"quoted"`, want: "&quot;quoted&quot;"},
		{in: "'apos'", want: "&apos;apos&apos;"},
		{in: `a & b < c > d "e" 'f'`, want: "a &amp; b &lt; c &gt; d &quot;e&quot; &apos;f&apos;"},
		{in: "&amp;", want: "&amp;amp;"}, // existing entities are not special-cased
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, escapeXML(tt.in))
		})
	}
}

func TestBundledResourcesHint(t *testing.T) {
	t.Parallel()

	writeEntries := func(t *testing.T, dir string, names ...string) {
		t.Helper()
		require.NoError(t, os.MkdirAll(dir, 0o755))
		for _, name := range names {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
		}
	}

	t.Run("nil skill renders no hint", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, bundledResourcesHint(nil))
	})

	t.Run("empty path renders no hint", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, bundledResourcesHint(&Skill{}))
	})

	t.Run("missing directory renders no hint", func(t *testing.T) {
		t.Parallel()
		skill := &Skill{Path: filepath.Join(t.TempDir(), "does-not-exist")}
		require.Empty(t, bundledResourcesHint(skill))
	})

	t.Run("resource dirs with no entries are omitted", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeEntries(t, filepath.Join(dir, "scripts")) // exists but empty
		require.Empty(t, bundledResourcesHint(&Skill{Path: dir}))
	})

	t.Run("entries are counted per resource dir in fixed order", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeEntries(t, filepath.Join(dir, "assets"), "a.png", "b.png", "c.png")
		writeEntries(t, filepath.Join(dir, "scripts"), "run.sh")
		writeEntries(t, filepath.Join(dir, "references"), "r1.md", "r2.md")
		require.Equal(t, "[bundled: scripts:1, references:2, assets:3]", bundledResourcesHint(&Skill{Path: dir}))
	})

	t.Run("only non-empty dirs are listed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeEntries(t, filepath.Join(dir, "references"), "r.md")
		require.Equal(t, "[bundled: references:1]", bundledResourcesHint(&Skill{Path: dir}))
	})
}

func TestToPromptIndex(t *testing.T) {
	t.Parallel()

	t.Run("empty set renders empty string", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, ToPromptIndex(nil))
		require.Empty(t, ToPromptIndex([]*Skill{}))
	})

	t.Run("renders one line per skill with compat and bundled hint", func(t *testing.T) {
		t.Parallel()
		skillDir := filepath.Join(t.TempDir(), "my-skill")
		scriptsDir := filepath.Join(skillDir, "scripts")
		require.NoError(t, os.MkdirAll(scriptsDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "run.sh"), []byte("x"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(scriptsDir, "util.sh"), []byte("x"), 0o644))
		skillFile := filepath.Join(skillDir, "SKILL.md")
		skill := &Skill{
			Name:          "my-skill",
			Description:   "Does things",
			Compatibility: "tau",
			Path:          skillDir,
			SkillFilePath: skillFile,
		}
		want := "<available_skills>\n" +
			"  - my-skill: Does things (" + filepath.ToSlash(skillFile) + ") [compat: tau] [bundled: scripts:2]\n" +
			"</available_skills>"
		require.Equal(t, want, ToPromptIndex([]*Skill{skill}))
	})

	t.Run("escapes xml in name and description", func(t *testing.T) {
		t.Parallel()
		skill := &Skill{Name: `a<b`, Description: `say "hi" & 'bye'`, SkillFilePath: "/x/y/SKILL.md"}
		want := "<available_skills>\n" +
			"  - a&lt;b: say &quot;hi&quot; &amp; &apos;bye&apos; (/x/y/SKILL.md)\n" +
			"</available_skills>"
		require.Equal(t, want, ToPromptIndex([]*Skill{skill}))
	})

	t.Run("skips nil and disable-model-invocation skills", func(t *testing.T) {
		t.Parallel()
		on := &Skill{Name: "on", Description: "d", SkillFilePath: "/skills/on/SKILL.md"}
		off := &Skill{Name: "off", Description: "d", DisableModelInvocation: true, SkillFilePath: "/skills/off/SKILL.md"}
		got := ToPromptIndex([]*Skill{nil, off, on})
		require.Equal(t, "<available_skills>\n  - on: d (/skills/on/SKILL.md)\n</available_skills>", got)
	})

	t.Run("multiple skills keep input order", func(t *testing.T) {
		t.Parallel()
		a := &Skill{Name: "a", Description: "da", SkillFilePath: "/s/a/SKILL.md"}
		b := &Skill{Name: "b", Description: "db", SkillFilePath: "/s/b/SKILL.md"}
		got := ToPromptIndex([]*Skill{a, b})
		require.Equal(t,
			"<available_skills>\n  - a: da (/s/a/SKILL.md)\n  - b: db (/s/b/SKILL.md)\n</available_skills>",
			got)
	})
}

func TestToPromptXML(t *testing.T) {
	t.Parallel()

	t.Run("empty set renders empty string", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, ToPromptXML(nil))
		require.Empty(t, ToPromptXML([]*Skill{}))
	})

	t.Run("renders a skill element per skill", func(t *testing.T) {
		t.Parallel()
		skillFile := filepath.Join(t.TempDir(), "my-skill", "SKILL.md")
		skill := &Skill{Name: "my-skill", Description: "Does things", SkillFilePath: skillFile}
		want := "<available_skills>\n" +
			"  <skill>\n" +
			"    <name>my-skill</name>\n" +
			"    <description>Does things</description>\n" +
			"    <location>" + filepath.ToSlash(skillFile) + "</location>\n" +
			"  </skill>\n" +
			"</available_skills>"
		require.Equal(t, want, ToPromptXML([]*Skill{skill}))
	})

	t.Run("escapes xml in fields", func(t *testing.T) {
		t.Parallel()
		skill := &Skill{Name: `a<b`, Description: `say "hi" & 'bye'`, SkillFilePath: "/x/y/SKILL.md"}
		want := "<available_skills>\n" +
			"  <skill>\n" +
			"    <name>a&lt;b</name>\n" +
			"    <description>say &quot;hi&quot; &amp; &apos;bye&apos;</description>\n" +
			"    <location>/x/y/SKILL.md</location>\n" +
			"  </skill>\n" +
			"</available_skills>"
		require.Equal(t, want, ToPromptXML([]*Skill{skill}))
	})

	t.Run("skips nil and disable-model-invocation skills", func(t *testing.T) {
		t.Parallel()
		on := &Skill{Name: "on", Description: "d", SkillFilePath: "/skills/on/SKILL.md"}
		off := &Skill{Name: "off", Description: "d", DisableModelInvocation: true, SkillFilePath: "/skills/off/SKILL.md"}
		got := ToPromptXML([]*Skill{nil, off, on})
		require.Equal(t, "<available_skills>\n  <skill>\n    <name>on</name>\n    <description>d</description>\n    <location>/skills/on/SKILL.md</location>\n  </skill>\n</available_skills>", got)
	})
}

func TestAdditionalSources(t *testing.T) {
	t.Parallel()

	t.Run("nil paths produce no sources", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, AdditionalSources(nil, ScopeUser))
	})

	t.Run("blank paths are skipped and scope priority applied", func(t *testing.T) {
		t.Parallel()
		sources := AdditionalSources([]string{"", "   ", "/x", "  /y  "}, ScopeProject)
		require.Equal(t, []Source{
			{Root: "/x", Scope: ScopeProject, Priority: extraPathPriority},
			{Root: "/y", Scope: ScopeProject, Priority: extraPathPriority},
		}, sources)
	})

	t.Run("scope is preserved", func(t *testing.T) {
		t.Parallel()
		sources := AdditionalSources([]string{"/only"}, ScopeBuiltin)
		require.Equal(t, []Source{{Root: "/only", Scope: ScopeBuiltin, Priority: extraPathPriority}}, sources)
	})
}

func TestCloneSkill(t *testing.T) {
	t.Parallel()

	t.Run("nil clones to nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, cloneSkill(nil))
	})

	t.Run("all fields are copied", func(t *testing.T) {
		t.Parallel()
		original := &Skill{
			Name:                   "a",
			Description:            "d",
			UserInvocable:          true,
			DisableModelInvocation: true,
			License:                "MIT",
			Compatibility:          "tau",
			Metadata:               map[string]string{"k": "v"},
			AllowedTools:           "bash",
			Enabled:                true,
			Instructions:           "i",
			Path:                   "/p",
			SkillFilePath:          "/p/SKILL.md",
			Scope:                  ScopeUser,
			SourceRoot:             "/p",
			Priority:               30,
		}
		clone := cloneSkill(original)
		require.NotSame(t, original, clone)
		require.Equal(t, *original, *clone)
	})

	t.Run("metadata map is deep copied", func(t *testing.T) {
		t.Parallel()
		original := &Skill{Name: "a", Metadata: map[string]string{"k": "v"}}
		clone := cloneSkill(original)
		require.NotNil(t, clone.Metadata)
		clone.Metadata["k"] = "changed"
		clone.Metadata["new"] = "x"
		require.Equal(t, "v", original.Metadata["k"])
		require.NotContains(t, original.Metadata, "new")
	})

	t.Run("nil metadata stays nil", func(t *testing.T) {
		t.Parallel()
		clone := cloneSkill(&Skill{Name: "a"})
		require.NotNil(t, clone)
		require.Nil(t, clone.Metadata)
	})
}

func TestCloneSkills(t *testing.T) {
	t.Parallel()

	t.Run("nil slice clones to nil", func(t *testing.T) {
		t.Parallel()
		require.Nil(t, cloneSkills(nil))
	})

	t.Run("empty slice clones to empty non-nil slice", func(t *testing.T) {
		t.Parallel()
		out := cloneSkills([]*Skill{})
		require.NotNil(t, out)
		require.Empty(t, out)
	})

	t.Run("entries are cloned in order and nil entries stay nil", func(t *testing.T) {
		t.Parallel()
		in := []*Skill{{Name: "a"}, nil, {Name: "b"}}
		out := cloneSkills(in)
		require.Len(t, out, 3)
		require.Equal(t, "a", out[0].Name)
		require.Nil(t, out[1])
		require.Equal(t, "b", out[2].Name)
		require.NotSame(t, in[0], out[0])
		require.NotSame(t, in[2], out[2])
	})
}
