# Security Policy

## Supported versions

skit is currently released as `v0.x` (see [RELEASING.md](RELEASING.md) for the
versioning policy). Until a `v1.0.0` release, security fixes land on the latest
released tag only.

| Version           | Supported |
| ----------------- | --------- |
| latest `v0.x` tag | ✅        |
| older tags        | ❌        |

## Reporting a vulnerability

Please report security vulnerabilities **privately**. Do not open a public
issue or pull request, and do not disclose details publicly until a fix has
been released.

Use GitHub's private vulnerability reporting:

1. Open the repository's **Security** tab → **Report a vulnerability**.
2. Include the affected package and version(s), the impact, and a reproduction
   if possible.

If private reporting is unavailable to you, contact the maintainer via the
address on the repository's GitHub profile.

We aim to acknowledge a report within a few business days and to share a
remediation timeline after triage.

## Scope

skit ships security-sensitive building blocks — notably `auth` (JWT
verification, JWKS, RBAC), `errs` (secret sanitization), and `dbx` (query
construction). Reports touching these are especially welcome. Please note
whether the issue is exploitable in a default configuration.

## Disclosure

We follow coordinated disclosure: once a fix is released we publish an advisory,
crediting the reporter unless anonymity is requested.
