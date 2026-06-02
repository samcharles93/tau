# 23. Skill Installation via CLI

## Status: Not yet planned

### Motivation

Skills are discovered from filesystem paths but there's no `tau skill install` command to fetch skills from remote sources (GitHub repos, URLs, or a skill registry).

### Design

```shell
tau skill install <source>          # install from URL or GitHub path
tau skill install gh:user/repo      # shorthand for GitHub
tau skill install ./path/to/skill   # local path
tau skill list                      # list installed skills with status
tau skill remove <name>             # uninstall a skill
tau skill update [name]             # update skill(s)
```

- Skills installed to `~/.tau/skills/` (user) or `.tau/skills/` (project)
- Metadata stored alongside skill for update tracking (source URL, version, installed_at)
- `tau.yaml` skill manifest for version/update info

### Open question

Should skills be versioned (git tags) or always latest? A lockfile (`tau-skills.lock`) similar to npm/pip would pin versions.
