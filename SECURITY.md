# Security policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Use GitHub's private
security advisory flow on this repository (Security → Advisories → Report a
vulnerability), or contact the maintainers through the repository owner.

We aim to acknowledge reports within a few days and will coordinate a fix and
disclosure timeline with you.

## Threat model

Open Voice Bridge is a local-first agent:

- The admin UI and API bind to `127.0.0.1` only and reject non-loopback hosts.
- POST requests are checked against the request origin.
- API keys and provider tokens are stored in files with mode `0600`; token
  contents are never written to logs or returned by the API.
- The root helper (`xiaomi-remote remap`) exposes a Unix socket for key
  injection. It does not listen on the network.

Areas to keep in mind when contributing:

- Any new network listener must be separate from the admin UI, authenticated and
  documented. The planned phone input uses its own TLS listener with pairing.
- Never log audio, transcripts, API keys or pairing tokens.
- Treat ASR/LLM endpoints as untrusted inputs; validate sizes and timeouts.
- The remap helper must only accept known logical keys from trusted callers.

## Supported versions

Security fixes target the latest `main` branch. There are no long-term support
branches yet.