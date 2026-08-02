# Domain Entity

* [Environment Variable](/concepts/environment-variable.md) - KEY=value secret stored in the vault; masked by default everywhere it is displayed.
* [Project Environment](/concepts/project-environment.md) - Named environment (dev/prod/staging) inside a project, each mapped to its own .env path.
* [Trusted Operation](/concepts/trusted-operation.md) - Owner-configured, opaque HTTP operation that binds one vault secret to a fixed request without exposing the secret name or value to an agent.
* [Vault](/concepts/vault.md) - Single encrypted file holding every project's environment variables.
