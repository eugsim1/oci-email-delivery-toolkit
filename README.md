# OCI Email Delivery toolkit

An implementation-ready package for sending application email through Oracle Cloud Infrastructure (OCI) Email Delivery. It combines training material, an editable architecture diagram, a detailed configuration runbook, and a dependency-free Go SMTP client.

> **Disclaimer:** This project is independent and is not affiliated with, endorsed by, or supported by Oracle or any Oracle product team. Validate all settings, limits, prices, and security requirements against the current official Oracle documentation and your organization's policies before production use.

![OCI Email Delivery architecture](docs/architecture/oci-email-delivery-architecture.png)

The editable source is available as [a draw.io diagram](docs/architecture/oci-email-delivery-architecture.drawio).

## What is included

| Path | Purpose |
| --- | --- |
| `presentations/01-oci-email-delivery-foundations.pptx` | Service concepts, architecture, IAM, regionality, and limits |
| `presentations/02-oci-email-delivery-setup.pptx` | Console setup, DNS authentication, approved senders, TLS, and validation |
| `presentations/03-oci-email-delivery-operations-and-go.pptx` | Go integration, secrets, observability, reputation, and troubleshooting |
| `docs/oci-email-delivery-setup.md` | Complete implementation and operations runbook |
| `docs/architecture/oci-email-delivery-architecture.drawio` | Editable system architecture |
| `cmd/oci-smtp-mailer` | Go SMTP client supporting TLS and STARTTLS |
| `.env.example` | Configuration template containing no credentials |
| `.github/workflows/release.yml` | Test-gated, commit-based GitHub release automation |

## Architecture and service model

The application connects to the public SMTP endpoint for one OCI region. It authenticates with Oracle-generated SMTP credentials belonging to a dedicated IAM user and sends from an approved address or DKIM-enabled domain. OCI evaluates authorization and suppression status, accepts the message, and relays it to the recipient's mail provider.

| Component | Scope | Role |
| --- | --- | --- |
| Email domain | Regional | Establishes a sending domain and hosts DKIM configuration |
| Approved sender | Regional | Authorizes an exact `From` address when domain-based sending is not used |
| SMTP endpoint | Regional | Receives authenticated TLS SMTP submissions |
| SMTP credentials | IAM/global | Username and password generated for an IAM user; not the Console password |
| SPF, DKIM, DMARC | DNS | Authenticate mail and state the domain policy |
| Suppression list | Regional | Blocks delivery to known problematic recipient addresses |
| Metrics and logs | Regional | Show accepted, relayed, bounced, complained, and failed messages |

OCI Email Delivery is a sending service rather than an inbox service: it does not provide IMAP/POP mailboxes or receive replies. Resources and endpoints are regional, so create and operate the email domain, approved sender, logs, suppression entries, and SMTP endpoint in the same intended region.

## Prerequisites

- An OCI tenancy and access to the target region.
- Permission to manage users or groups, policies, Email Delivery resources, and logs.
- Administrative access to the sending domain's public DNS.
- Go 1.21 or later for the sample client.
- Outbound network access to the OCI endpoint, normally TCP port `465` for implicit TLS.
- A test recipient that you are authorized to contact.

## Clone the repository

```bash
git clone https://github.com/eugsim1/oci-email-delivery-toolkit.git
cd oci-email-delivery-toolkit
```

## End-to-end setup

The full click-by-click and command-level procedure is in [the configuration runbook](docs/oci-email-delivery-setup.md). The required sequence is:

1. **Choose the region and compartment.** Keep the SMTP endpoint, email domain, approved sender, logging, and operational checks aligned to that region.
2. **Create a dedicated IAM identity.** Avoid using a personal administrator. Add the identity to a narrowly scoped group.
3. **Create IAM policies.** Grant only the permissions needed to send email and, separately, to administer domains, approved senders, suppressions, logs, or metrics.
4. **Generate SMTP credentials.** In the IAM user's resources, generate an SMTP credential and immediately store the username and password in an approved secret manager. The password is shown only when generated.
5. **Create the email domain.** Add the sending domain in OCI Email Delivery in the target region and compartment.
6. **Enable logs before testing.** Enable both `OutboundAccepted` and `OutboundRelayed` logs on the email domain so acceptance and downstream relay can be distinguished.
7. **Publish SPF.** Use the exact regional include value displayed by OCI. A domain must have only one SPF TXT record; merge mechanisms instead of adding a second record.
8. **Configure DKIM.** Create a DKIM selector in OCI, publish the exact CNAME record in DNS, and wait for OCI to report it as active.
9. **Publish DMARC.** Begin with a monitoring policy if appropriate for the organization, review reports, then increase enforcement deliberately.
10. **Create an approved sender.** Authorize the exact `From` address, unless the selected OCI configuration supports sending from the verified DKIM domain without one.
11. **Copy the public SMTP endpoint.** Obtain it from the regional Email Delivery Configuration page. Do not construct or guess it.
12. **Configure and run the client.** Use TLS, the Oracle-generated credentials, and a `From` address authorized in the same region.
13. **Validate delivery.** Confirm client success, `OutboundAccepted`, `OutboundRelayed`, recipient arrival, and SPF/DKIM/DMARC results in the received message headers.

## IAM guidance

Separate sending from administration. A workload identity generally needs only permission to use approved senders. Operators may separately need permission to manage email domains, approved senders, suppressions, logging configuration, and metrics. Scope policies to the smallest practical compartment and use your tenancy's current policy syntax from the official documentation.

Do not grant broad tenancy-wide administration merely to send mail. Where the workload runs on OCI, evaluate resource principals or instance principals for other OCI API calls; SMTP submission itself still uses SMTP credentials.

## DNS authentication

### SPF

Publish the TXT value shown in the OCI Console for the selected region. SPF authorizes the OCI sending infrastructure to send on behalf of the envelope domain. There must be one syntactically valid SPF record at a domain; if other providers send mail for the same domain, combine their mechanisms and stay within SPF lookup limits.

### DKIM

OCI provides a selector and CNAME target. Publish both exactly as supplied, then wait for DNS propagation and OCI activation. DKIM cryptographically signs messages and supports domain alignment for DMARC.

### DMARC

DMARC evaluates alignment between the visible `From` domain and SPF and/or DKIM. A staged rollout such as monitoring, quarantine, then reject may reduce accidental disruption. Route aggregate reports to a controlled mailbox or reporting service and review them.

## Configure the Go client

The program reads environment variables directly; it intentionally does not load `.env` files. Use a secret manager or your platform's protected environment configuration in production.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `OCI_SMTP_HOST` | Yes | — | Exact regional public SMTP endpoint copied from OCI |
| `OCI_SMTP_PORT` | No | `465` | SMTP port |
| `OCI_SMTP_MODE` | No | `tls` | `tls` for implicit TLS or `starttls` |
| `OCI_SMTP_USERNAME` | Yes | — | Oracle-generated SMTP username |
| `OCI_SMTP_PASSWORD` | Yes | — | Oracle-generated SMTP password |
| `OCI_EMAIL_FROM` | Yes | — | Authorized sender address |
| `OCI_EMAIL_TO` | Yes | — | Comma-separated recipient list |
| `OCI_EMAIL_CC` | No | empty | Comma-separated CC list |
| `OCI_EMAIL_SUBJECT` | No | `OCI Email Delivery test` | Message subject; CR/LF is rejected |
| `OCI_EMAIL_TEXT` | Conditional | empty | Plain-text body; set this, HTML, or both |
| `OCI_EMAIL_HTML` | Conditional | empty | HTML body; set this, text, or both |
| `OCI_SMTP_TIMEOUT` | No | `30s` | Positive Go duration for network operations |

Copy `.env.example` as a reference, but never place real credentials in a committed file.

### PowerShell example

```powershell
$env:OCI_SMTP_HOST = 'smtp.email.eu-frankfurt-1.oci.oraclecloud.com'
$env:OCI_SMTP_PORT = '465'
$env:OCI_SMTP_MODE = 'tls'
$env:OCI_SMTP_USERNAME = '<oracle-generated-smtp-user>'
$env:OCI_SMTP_PASSWORD = '<oracle-generated-smtp-password>'
$env:OCI_EMAIL_FROM = 'no-reply@example.com'
$env:OCI_EMAIL_TO = 'recipient@example.net'
$env:OCI_EMAIL_CC = ''
$env:OCI_EMAIL_SUBJECT = 'OCI Email Delivery test'
$env:OCI_EMAIL_TEXT = 'Hello from OCI Email Delivery.'
$env:OCI_EMAIL_HTML = ''
$env:OCI_SMTP_TIMEOUT = '30s'

go run ./cmd/oci-smtp-mailer
```

### Linux or macOS example

```bash
export OCI_SMTP_HOST='smtp.email.eu-frankfurt-1.oci.oraclecloud.com'
export OCI_SMTP_PORT='465'
export OCI_SMTP_MODE='tls'
export OCI_SMTP_USERNAME='<oracle-generated-smtp-user>'
export OCI_SMTP_PASSWORD='<oracle-generated-smtp-password>'
export OCI_EMAIL_FROM='no-reply@example.com'
export OCI_EMAIL_TO='recipient@example.net'
export OCI_EMAIL_SUBJECT='OCI Email Delivery test'
export OCI_EMAIL_TEXT='Hello from OCI Email Delivery.'
export OCI_SMTP_TIMEOUT='30s'

go run ./cmd/oci-smtp-mailer
```

A successful submission prints:

```text
message accepted for delivery to 1 recipient(s)
```

This confirms SMTP acceptance, not inbox placement. Check OCI relay logs and the receiving system afterward.

### Multiple recipients and HTML

Use comma-separated address lists. Set both bodies to generate a `multipart/alternative` message:

```powershell
$env:OCI_EMAIL_TO = 'first@example.net,second@example.net'
$env:OCI_EMAIL_CC = 'audit@example.net'
$env:OCI_EMAIL_TEXT = 'This is the plain-text version.'
$env:OCI_EMAIL_HTML = '<p>This is the <strong>HTML</strong> version.</p>'
go run ./cmd/oci-smtp-mailer
```

### Dry run

Inspect the generated MIME message without contacting OCI:

```bash
go run ./cmd/oci-smtp-mailer -dry-run
```

The dry run still validates configuration and therefore requires placeholder SMTP credentials, but it does not print the password or open a network connection.

## Client security behavior

- Requires TLS 1.2 or newer and validates the SMTP server certificate.
- Supports implicit TLS and STARTTLS; it never silently falls back to plaintext.
- Rejects CR/LF in headers to prevent header injection.
- Parses and validates mailbox addresses before connecting.
- Generates MIME-safe subjects, bodies, boundaries, dates, and message IDs.
- Does not log SMTP credentials or message bodies during a normal send.
- Uses configurable connection and command deadlines.

## Build and test

```bash
go test ./...
go vet ./...
go build -o oci-smtp-mailer ./cmd/oci-smtp-mailer
```

No third-party Go modules are required. Tests cover address parsing, header-injection rejection, multipart construction, validation, and configuration defaults.

## Delivery validation

Validate each layer independently:

1. The client completes SMTP authentication and receives an acceptance response.
2. `OutboundAccepted` contains the submission.
3. `OutboundRelayed` records the downstream relay outcome.
4. The recipient receives the message, including in spam/junk checks.
5. The received headers report expected SPF, DKIM, and DMARC results.
6. Bounces and complaints remain within organizational thresholds.

An accepted SMTP transaction is not a guarantee of delivery. Recipient providers can defer, filter, reject, or place mail in junk folders.

## Limits and capacity planning

OCI applies service limits and reputation controls that can vary by tenancy, region, account history, and service updates. Review the Console and current official documentation rather than hard-coding assumptions.

Plan for:

- approved-sender and domain counts;
- daily or rolling sending volume;
- maximum recipients per message;
- message size and attachment expansion from base64 encoding;
- concurrent SMTP connections and submission rate;
- suppression-list effects and retry behavior.

For high-volume workloads, queue messages, apply bounded concurrency, use exponential backoff with jitter for temporary failures, and make retries idempotent. Do not retry permanent authentication, authorization, or invalid-recipient errors indefinitely.

## Monitoring and operations

- Enable `OutboundAccepted` and `OutboundRelayed` logs before production traffic.
- Monitor accepted, relayed, bounced, complained, and failed message counts and ratios.
- Alert on authentication failures, sudden bounce/complaint increases, sustained throttling, and unexpected volume changes.
- Correlate application message IDs with OCI logs without exposing recipient data unnecessarily.
- Inspect the suppression list when valid recipients stop receiving messages. Remove an entry only after fixing the underlying problem and confirming the address should receive mail.
- Warm new domains and sending patterns gradually. Maintain consent, unsubscribe handling, list hygiene, and predictable traffic.

## Credential rotation

OCI permits a limited number of SMTP credentials per IAM user. A safe rotation is:

1. Generate a second credential.
2. Store it in the approved secret manager.
3. Deploy the new credential without deleting the old one.
4. Send and trace a controlled test message.
5. Confirm the new credential is active everywhere.
6. Delete the old credential and record the rotation.

Never commit SMTP credentials, paste them into tickets or presentations, or reuse the OCI Console password.

## Troubleshooting

| Symptom | Likely cause | Checks and action |
| --- | --- | --- |
| `535 Authentication failed` | Wrong credential type, stale secret, or copied value | Use the Oracle-generated SMTP username/password; rotate if uncertain |
| Sender not authorized | Sender or domain not approved in the endpoint's region | Verify the exact `From`, email domain/DKIM status, compartment, and region |
| TLS/connect timeout | Egress, firewall, proxy, DNS, endpoint, or port issue | Test name resolution and TCP 465; copy the endpoint from OCI |
| Accepted but not received | Relay failure, suppression, filtering, or reputation | Check both OCI log types, suppression list, bounce response, and recipient junk folder |
| SPF fail | Incorrect or duplicated SPF record | Query public DNS and consolidate to one valid SPF TXT record |
| DKIM fail | Wrong selector/target or propagation delay | Compare the published CNAME exactly with OCI and recheck activation |
| DMARC fail | Visible `From` is not aligned with authenticated domain | Inspect message headers and align SPF and/or DKIM with the `From` domain |
| Throttling | Current rate or volume exceeds an applicable limit | Queue, back off with jitter, reduce concurrency, and review service limits |

The [full runbook](docs/oci-email-delivery-setup.md) contains a longer troubleshooting matrix and production-readiness checklist.

## Release automation

Every push to `main` runs tests and `go vet`. After they succeed, the workflow creates exactly one GitHub release for that commit:

- the tag is `commit-<full-git-sha>`, so reruns are idempotent;
- the title and notes come from the newest `##` section in `CHANGELOG.md`;
- the release includes a source ZIP generated from the tested commit;
- only the release job receives `contents: write`; all other workflow permissions are read-only.

## Official references

- [OCI Email Delivery documentation](https://docs.oracle.com/en-us/iaas/Content/Email/home.htm)
- [Getting started with Email Delivery](https://docs.oracle.com/en-us/iaas/Content/Email/Concepts/overview.htm)
- [Configuring SMTP connection](https://docs.oracle.com/en-us/iaas/Content/Email/Tasks/configuresmtpconnection.htm)
- [Managing approved senders](https://docs.oracle.com/en-us/iaas/Content/Email/Tasks/managingapprovedsenders.htm)
- [Managing email domains](https://docs.oracle.com/en-us/iaas/Content/Email/Tasks/managingemaildomains.htm)
- [Email Delivery metrics](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/emailmetrics.htm)

## License

Released under the [MIT License](LICENSE).
