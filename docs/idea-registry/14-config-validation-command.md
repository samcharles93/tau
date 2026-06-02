# 14. Config Validation Command (`tau config validate`)

## Status: Not yet planned

### Motivation

Users configure providers in YAML and currently discover errors only at startup. A `tau config validate` CLI command would let users verify their config without launching a full session, and could be used in CI/CD or pre-commit hooks.

### Design

```shell
tau config validate          # validate global + project config
tau config validate --global # validate global only
tau config validate --json   # output as JSON with field-level errors
tau config path              # print config file paths being used
```

Output:

```shell
✓ global config: /home/user/.config/tau/config.yaml
✓ project config: /work/apps/tau/.tau.yaml
✓ provider "anthropic": valid (oauth_pkce)
⚠ provider "openai": api_key_env MYSECRET_API_KEY is empty or unset
✓ 2 providers, 0 errors, 1 warning
```
