# STRICT REQUIREMENTS

- Never hard code any values, paths, or configurations in the code. Always use environment variables or configuration files to manage such information.
- Ensure that all sensitive information like API keys, tokens or passwords are stored securely and not exposed in the codebase or logs and anything in the project repository must be ignored via .gitignore.
- This codebase uses gofumpt for formatting. All code must be formatted using gofumpt to maintain consistency and readability.
- All code must pass `golangci-lint` and `go fix ./...` checks before being committed to the repository. This ensures that the code adheres to best practices and is free of common issues.
