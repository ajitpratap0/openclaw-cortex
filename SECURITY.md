# Security Policy

## Reporting a Vulnerability

Please report security vulnerabilities through the GitHub Security Advisory tab:

**[Report a vulnerability](https://github.com/ajitpratap0/openclaw-cortex/security/advisories/new)**

Do not open a public GitHub issue for security vulnerabilities.

### What to include

- Description of the vulnerability and its potential impact
- Steps to reproduce (proof of concept if possible)
- Affected versions
- Any suggested mitigations

### Response SLA

- **Acknowledgement**: within 14 days of submission
- **Fix**: best-effort; critical issues prioritized

## Supported Versions

Only the latest commit on `main` receives security fixes.

| Version | Supported |
|---------|-----------|
| `main` (latest) | Yes |
| Older tags | No |

## Scope

In scope:
- Memory extraction prompt injection (`internal/capture/`)
- Authentication bypass in the HTTP API (`internal/api/`)
- Path traversal in the indexer (`internal/indexer/`)
- Arbitrary command execution via config loading

Out of scope:
- Vulnerabilities in Memgraph, Ollama, or the Anthropic API themselves
- Issues requiring physical access to the host machine
- Denial-of-service attacks against local services

## Disclosure

After a fix is merged, we will publish a GitHub Security Advisory with full details.
