# Tau

Provider-agnostic, OpenAI-compatible chat client with an interactive terminal
UI, agentic tool-calling, and session persistence.

## Quick start

```bash
# Build
go build -o tau ./cmd/tau

# Configure
cat > .tau.yaml <<EOF
default_provider: deepseek
default_model: deepseek-v4-flash

providers:
  - name: deepseek
    base_url: https://api.deepseek.com
    auth:
      type: api_key
      api_key_env: DEEPSEEK_API_KEY
EOF

# Chat
./tau

# Single-shot
./tau -p "Explain the architecture of this codebase"
```

## Documentation

* [CLI reference](docs/README.md)
* [AI SDK integration and model catalog](docs/ai-sdk.md)
* [Event bus design](docs/eventbus.md)
* Example configs:
  * [Generic](docs/config-example.yaml)
  * [DeepSeek](docs/config-deepseek-example.yaml)

## Development

```bash
task check    # lint + format + test
```
