# Security

Ungit-Go executes Git commands and can modify repositories, so treat access to a running instance as access to the repositories it can reach.

## Safe defaults

The default bind address is localhost. Keep it that way unless remote access is intentional.

If exposing Ungit-Go beyond localhost:

- enable authentication;
- restrict `allowedIPs` where practical;
- use a trusted reverse proxy with TLS;
- protect the host account and Git credentials;
- do not expose development/testing mode.

## Telemetry

Ungit-Go does not ship the inherited upstream Raven/Sentry reporting endpoint. The legacy `bugtracking` configuration field may remain accepted for compatibility, but Ungit-Go does not use it to send reports to the original Ungit project.

## Reporting a vulnerability

Please report security issues privately to the Ungit-Go maintainer rather than opening a public issue with exploit details. Add a project security contact/mechanism before the first public release if your hosting platform supports private vulnerability reports.
