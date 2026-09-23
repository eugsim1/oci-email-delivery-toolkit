# Changelog

## 1.3.0 - 2026-09-23

- Added a simple Bash and curl alternative for plain-text OCI Email Delivery submissions over implicit TLS on port 465.
- Added dry-run validation, multiple To/CC recipients, header-injection checks, and protected temporary credential handling.
- Added Bash tests and README examples for interactive use, multiple recipients, protected environment files, and MIME inspection.

## 1.2.0 - 2026-09-22

- Added repeatable Linux attachment support to the Go SMTP client with MIME media-type detection and base64 encoding.
- Added encoded-message size enforcement, regular-file and duplicate checks, and safe attachment filenames.
- Documented all attachment flags, environment variables, Linux examples, dry-run inspection, security behavior, and troubleshooting steps.

## 1.1.1 - 2026-09-22

- Updated the GitHub Actions checkout and Go setup steps to Node.js 24-compatible major versions.

## 1.1.0 - 2026-09-22

- Expanded the README with every OCI Console, IAM, DNS, logging, sender, TLS, limits, and validation step required for Email Delivery.
- Made Linux the primary documented runtime with build, secure secret-loading, connectivity, and execution procedures.
- Added a production-readiness checklist and refreshed the official Oracle reference links.

## 1.0.0 - 2026-09-22

- Added three OCI Email Delivery training presentations.
- Added a detailed Console, DNS, SMTP, monitoring, and troubleshooting runbook.
- Added an editable draw.io architecture diagram and rendered preview.
- Added a dependency-free Go SMTP client with text, HTML, multiple-recipient, TLS, validation, and dry-run support.
- Added unit tests, security guidance, an MIT license, and automated commit-based GitHub releases.
