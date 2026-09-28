# Security Policy

## Reporting a vulnerability

If you believe you have found a security vulnerability in dify-go-sdk, please
report it privately through GitHub Security Advisories:

https://github.com/langgenius/dify-go-sdk/security/advisories/new

Please do not report security vulnerabilities through public issues,
discussions or pull requests.

Include as much as you can safely share: what the vulnerability is, how to
reproduce it, the SDK and Dify versions involved, and its potential impact.

A vulnerability in Dify itself, rather than in this client, belongs in
[Dify's own advisories](https://github.com/langgenius/dify/security/advisories/new).

## What this SDK holds

Two things here are credentials and deserve care in a report:

- **Service-API keys** (`app-…`, `dataset-…`), which a client holds and masks
  wherever it is printed.
- **A console session**, held by `Management`. It is the credential to a whole
  Dify account, since Dify offers nothing narrower; `Management.Logout` ends it.

## Supported versions

Only the latest release receives security fixes while the SDK is below 1.0.
