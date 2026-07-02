# Tau

## Project Overview

Tau is a provider-agnostic, coding agent with an interactive terminal UI.

### Windows Notes
- Bleve (search backend) uses mmap, which locks index files on Windows. Avoid deleting or renaming opened index files to prevent "sharing violation" errors.
- Always use forward slashes (`/`) for paths in configuration and code.

## Installation

```bash
# Install Task
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d

# Build Tau
task
```

## Configuration

Tau uses a YAML config file. By default, it looks for `.tau.yaml` in the current directory or `~/.config/tau/config.yaml`.

Example:

```yaml
# .tau.yaml
default_provider: deepseek
default_model: deepseek-v4-flash
```

See:
- [`docs/config-example.yaml`](docs/config-example.yaml)
- [`docs/config-deepseek-example.yaml`](docs/config-deepseek-example.yaml)
- [`docs/asyncapi/tau.yaml`](docs/asyncapi/tau.yaml).

## Documentation

* [CLI reference](docs/README.md)
* [Plugin SDK](docs/plugins.md)
* [AI SDK integration and model catalog](docs/ai-sdk.md)