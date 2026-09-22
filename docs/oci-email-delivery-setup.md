# Oracle Cloud Infrastructure Email Delivery: configuration runbook

This runbook describes a production-oriented SMTP setup for OCI Email Delivery and shows how to use the included Linux Go client. It was checked against Oracle documentation on 22 September 2026. OCI menus and account limits can change, so use the values shown in your tenancy when they differ from examples here.

> This project is independent and is not affiliated with, endorsed by, or supported by Oracle or any Oracle product team.

## 1. What OCI Email Delivery provides

OCI Email Delivery is a regional outbound email service for application-generated transactional and bulk email. Typical uses include receipts, account verification, password resets, alerts, and permitted marketing messages. It is not an inbox service and is not designed as a personal mailbox.

The service accepts submissions through:

- **SMTP**, authenticated with Oracle-generated SMTP credentials. This runbook and the Go client use SMTP.
- **HTTPS**, authenticated with OCI-native authentication. Oracle recommends considering HTTPS for new development because it offers more authentication options.

The service manages outbound transfer, shared or dedicated sending IPs, feedback loops, bounce collection, complaint collection, suppressions, metrics, and domain-level logs.

## 2. Components and scope

| Component | Purpose | Scope |
| --- | --- | --- |
| Email domain | Domain that you own and use for sending, DKIM, and logs | Regional |
| Approved sender | Exact `From` address, or `@domain` after DKIM becomes active | Regional and compartment-scoped |
| SMTP endpoint | Host that receives SMTP submissions | Regional |
| SMTP credentials | Oracle-generated username and password attached to an IAM user | Global identity credential |
| SPF record | DNS TXT record authorizing OCI sending infrastructure | DNS name and sending geography |
| DKIM | Cryptographic signature configured with an OCI-generated DNS record | Email domain and region |
| Suppression list | Addresses blocked after hard bounces, complaints, manual entries, or unsubscribe requests | Regional tenancy asset |
| Metrics | Aggregated delivery counts in namespace `oci_emaildelivery` | Regional; retained for 90 days according to Oracle documentation |
| Email domain logs | Detailed acceptance, relay, bounce, complaint, open, and click events | Regional; must be enabled |

Important design rule: the SMTP credential may authenticate in any region, but the email domain, approved sender, endpoint, logs, dashboard, and suppression list must align with the region used to send.

## 3. Prerequisites

Before opening the OCI Console, decide and record:

- OCI region, such as `eu-frankfurt-1`.
- Compartment that will contain Email Delivery resources. Avoid the root compartment for approved senders.
- Sending domain that your organization controls in public DNS, such as `mail.example.com`.
- `From` address, such as `no-reply@mail.example.com`.
- Whether one exact address or every address in the domain must send.
- IAM administrators who configure the service and the dedicated IAM user whose SMTP credentials the application will use.
- Secret store and credential-rotation procedure.
- Expected daily volume, peak rate, maximum encoded message size, and recipients' consent model.

Public mailbox-provider domains such as `gmail.com`, `hotmail.com`, or `yahoo.com` cannot serve as an OCI email domain because you do not control their DNS.

## 4. Create a dedicated IAM user and groups

Use a dedicated non-human IAM user for SMTP. Do not reuse an administrator's personal Console account.

1. In the OCI Console, open **Identity & Security**.
2. Open the identity domain that contains the application identities. For many tenancies this is the `Default` domain.
3. Create an administration group, for example `oci-email-admins`.
4. Create a sending group, for example `oci-email-senders`.
5. Create a dedicated user, for example `svc-email-prod`.
6. Add the dedicated user to `oci-email-senders`.
7. Confirm that the user capability **Can use SMTP credentials** is enabled.
8. Avoid granting a Console password or broad administration permissions to the service user unless another requirement needs them.

OCI allows a maximum of two SMTP credentials per IAM user. The password cannot be chosen by the customer, cannot be retrieved again after its creation dialog closes, and does not expire automatically.

## 5. Create IAM policies

Create policies in a location where the tenancy's IAM administrators can manage them. Replace the identity-domain, group, and compartment placeholders.

Minimum runtime permission for the SMTP user group:

```text
Allow group 'Default'/'oci-email-senders' to use email-family in compartment EmailPlatform
```

Administrative permissions for Email Delivery resources:

```text
Allow group 'Default'/'oci-email-admins' to manage email-family in compartment EmailPlatform
Allow group 'Default'/'oci-email-admins' to manage suppressions in tenancy
Allow group 'Default'/'oci-email-admins' to manage log-groups in compartment EmailPlatform
Allow group 'Default'/'oci-email-admins' to read log-content in compartment EmailPlatform
```

Optional policy from Oracle's getting-started example when administrators must manage other users' SMTP credentials:

```text
Allow group 'Default'/'oci-email-admins' to manage credentials in compartment EmailPlatform where target.credential.type = 'smtp'
```

Notes:

- Use the exact identity-domain syntax required by the tenancy. Oracle states that policies using SMTP credentials from a domain other than `Default` must include the identity-domain name.
- Suppressions require tenancy-level policy because the suppression list is a tenancy-level resource within its region.
- `use email-family` includes the `SmtpSend` permission through `APPROVED_SENDER_USE`.
- IAM changes usually propagate quickly, but a short delay can occur.

## 6. Generate SMTP credentials

Generate credentials on the dedicated service user:

1. Open **Identity & Security**, then locate the dedicated IAM user.
2. Open the user's **SMTP Credentials** resource.
3. Select **Generate SMTP Credentials**.
4. Enter a description that records the application, environment, region, and creation date.
5. Generate the credential.
6. Immediately copy both the generated SMTP username and SMTP password into the approved secret store.
7. Close the dialog only after verifying that the secret store contains both values.

Do not use any of the following in place of the SMTP credentials:

- OCI Console username or password.
- API key fingerprint or private key.
- Auth token.
- User OCID.

The generated SMTP username is usually a long Oracle-assigned value. Preserve it exactly.

## 7. Create the email domain

1. Select the region that will send the email.
2. Open **Developer Services**.
3. Under **Application Integration**, select **Email Delivery**.
4. Select **Email Domains**.
5. Select the intended compartment.
6. Select **Create Email Domain**.
7. Enter the domain that appears after `@` in the sending address, such as `mail.example.com`.
8. Add governance tags if your organization requires them.
9. Create the domain and record its OCID.

The domain must be publicly registered and controlled by your organization. Create a separate domain resource in each sending region.

## 8. Enable Email Delivery logs before testing

Enable logging before the first test so failed submissions and delivery events remain traceable.

1. Open the email domain.
2. Select **Logs**, or use the **Email Deliverability and Reputation Governance** dashboard.
3. Enable **OutboundAccepted** logging.
4. Enable **OutboundRelayed** logging.
5. Choose the appropriate log group and retention settings.
6. Confirm that administrators can query log content.

`OutboundAccepted` records successful submissions and submission failures, including invalid senders and suppressed recipients. `OutboundRelayed` records relay results, bounces, spam complaints, unsubscribes, opens, and clicks.

## 9. Configure SPF

SPF publishes the OCI infrastructure allowed to send for the envelope domain.

1. Inspect the domain's existing TXT records.
2. If an SPF TXT record already exists, add the applicable OCI `include` mechanism to that record. Do not publish two independent SPF records for the same DNS name.
3. Publish the TXT record at the sending domain through the authoritative DNS provider.
4. Wait for DNS propagation.
5. Verify the record from an external resolver.

Oracle currently documents these commercial-region values:

| Sending geography | SPF value |
| --- | --- |
| Americas | `v=spf1 include:rp.oracleemaildelivery.com ~all` |
| Asia/Pacific | `v=spf1 include:ap.rp.oracleemaildelivery.com ~all` |
| Europe | `v=spf1 include:eu.rp.oracleemaildelivery.com ~all` |
| All commercial regions | `v=spf1 include:rp.oracleemaildelivery.com include:ap.rp.oracleemaildelivery.com include:eu.rp.oracleemaildelivery.com ~all` |

For sovereign or government realms, use the realm-specific value shown in Oracle's SPF documentation and in the tenancy.

Example checks:

```bash
dig TXT mail.example.com
nslookup -type=TXT mail.example.com
```

## 10. Configure DKIM

DKIM signs mail so receiving systems can verify that OCI sent the message for the domain and that signed content was not modified.

1. Open the email domain in OCI Email Delivery.
2. Open **DKIM** and select **Add DKIM**.
3. Choose a selector name that supports future rotation, such as `oci2026a`.
4. Create the DKIM configuration.
5. Copy the DNS record name and target exactly as OCI displays them. OCI commonly uses a CNAME-based arrangement that simplifies key rotation.
6. Publish the record through the authoritative DNS provider.
7. Wait for DNS propagation.
8. Return to OCI and wait until DKIM signing shows an active state.
9. Send a test message and inspect its headers for `DKIM-Signature` and a passing authentication result.

The DKIM configuration applies only to approved senders whose exact domain matches the configured email domain. A DKIM configuration for `mail.example.com` does not automatically cover `example.com`.

## 11. Create an approved sender

Every visible `From` address must be authorized.

1. Keep the OCI Console on the intended sending region.
2. In Email Delivery, select **Approved Senders**.
3. Select the non-root compartment used for Email Delivery.
4. Select **Create Approved Sender**.
5. Enter the exact address, such as `no-reply@mail.example.com`.
6. Create the sender.
7. Wait briefly for propagation before the first test. If an immediate test returns an authorization error, retry with backoff.

To authorize every address in a domain:

1. Configure the email domain.
2. Activate DKIM signing for that domain.
3. Create the approved sender as `@mail.example.com`.

Keep the SMTP envelope sender aligned with the visible `From` address. Oracle discourages multiple `From` addresses and mismatched envelope/header senders because they harm DMARC alignment and deliverability.

## 12. Obtain the SMTP endpoint and TLS settings

1. In the selected region, open **Email Delivery**.
2. Select **Configuration**.
3. Copy the **Public endpoint** from the SMTP sending information panel.
4. Record the port and security mode shown by the tenancy.

Oracle's current documentation identifies port **465** as the standard submission port and requires encryption in transit. Port 465 negotiates TLS immediately when the connection starts, rather than upgrading a plaintext connection with STARTTLS.

Typical endpoint form:

```text
smtp.email.<region>.oci.oraclecloud.com
```

Example only:

```text
smtp.email.eu-frankfurt-1.oci.oraclecloud.com:465
```

Always copy the endpoint shown in the target region's Configuration page. Do not infer a sovereign-realm endpoint from the commercial pattern.

## 13. Configure and run the Go client

The included Linux client uses implicit TLS by default, authenticates with `AUTH PLAIN` only inside the encrypted connection, validates recipient syntax, blocks subject-header injection, and sends plain text, `multipart/alternative` content, and base64-encoded file attachments.

Required variables:

| Variable | Meaning |
| --- | --- |
| `OCI_SMTP_HOST` | Regional public SMTP endpoint without a port |
| `OCI_SMTP_USERNAME` | Oracle-generated SMTP username |
| `OCI_SMTP_PASSWORD` | Oracle-generated SMTP password |
| `OCI_EMAIL_FROM` | Approved `From` address |
| `OCI_EMAIL_TO` | Comma-separated recipients |
| `OCI_EMAIL_TEXT` or `OCI_EMAIL_HTML` | At least one message body |

Optional variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `OCI_SMTP_PORT` | `465` | SMTP submission port |
| `OCI_SMTP_MODE` | `tls` | `tls` for implicit TLS, or `starttls` if explicitly supported by the endpoint |
| `OCI_EMAIL_CC` | empty | Comma-separated carbon-copy recipients |
| `OCI_EMAIL_SUBJECT` | `OCI Email Delivery test` | Message subject |
| `OCI_EMAIL_ATTACHMENTS` | empty | Colon-separated Linux paths to regular files |
| `OCI_EMAIL_MAX_BYTES` | `2000000` | Maximum complete encoded MIME message in bytes |
| `OCI_SMTP_TIMEOUT` | `30s` | Overall connection and SMTP deadline |

Linux runtime:

```bash
cd <toolkit-directory>
export OCI_SMTP_HOST='smtp.email.eu-frankfurt-1.oci.oraclecloud.com'
export OCI_SMTP_PORT='465'
export OCI_SMTP_MODE='tls'
export OCI_SMTP_USERNAME='<oracle-generated-smtp-user>'
export OCI_SMTP_PASSWORD='<oracle-generated-smtp-password>'
export OCI_EMAIL_FROM='no-reply@mail.example.com'
export OCI_EMAIL_TO='recipient@example.net'
export OCI_EMAIL_SUBJECT='OCI Email Delivery validation'
export OCI_EMAIL_TEXT='This message validates the OCI SMTP configuration.'
export OCI_EMAIL_ATTACHMENTS=''
export OCI_EMAIL_MAX_BYTES='2000000'
go run ./cmd/oci-smtp-mailer
```

HTML plus text alternative:

```bash
export OCI_EMAIL_TEXT='Your report is ready.'
export OCI_EMAIL_HTML='<html><body><p>Your <strong>report</strong> is ready.</p></body></html>'
go run ./cmd/oci-smtp-mailer
```

Attachment flags:

| Flag | Meaning |
| --- | --- |
| `-attachment PATH` | Attach one regular file; repeat the flag for multiple files |
| `-max-message-bytes N` | Override the configured encoded-message limit for this run |
| `-dry-run` | Write the complete MIME message to standard output without an SMTP connection |

Send one or more attachments:

```bash
go run ./cmd/oci-smtp-mailer \
  -attachment /srv/reports/report.pdf \
  -attachment /srv/reports/summary.csv
```

The client uses each path's base filename in the MIME message, detects the media type from the extension, and falls back to `application/octet-stream`. It rejects missing paths, non-regular files, duplicate absolute paths, unsafe filenames, and a final encoded message larger than `OCI_EMAIL_MAX_BYTES`. Base64 expands binary data by about one third, so size the raw attachments conservatively.

For fixed Linux jobs, `OCI_EMAIL_ATTACHMENTS` accepts a colon-separated list:

```bash
export OCI_EMAIL_ATTACHMENTS='/srv/reports/report.pdf:/srv/reports/summary.csv'
go run ./cmd/oci-smtp-mailer
```

Flags append to the environment list. Oracle documents a 2 MB default message limit including headers and base64. Increase `OCI_EMAIL_MAX_BYTES` or use `-max-message-bytes` only after confirming that OCI approved a higher tenancy limit.

Inspect the MIME output without contacting OCI:

```bash
umask 077
go run ./cmd/oci-smtp-mailer \
  -attachment /srv/reports/report.pdf \
  -dry-run > /tmp/oci-message.eml
rm -f /tmp/oci-message.eml
```

Build and test:

```bash
go test ./...
go vet ./...
go build ./cmd/oci-smtp-mailer
```

## 14. Validate the complete path

After a successful test:

1. Confirm that the client reports acceptance by OCI.
2. Find an `OutboundAccepted` log entry for the sender and recipient.
3. Find the corresponding `OutboundRelayed` result.
4. Confirm receipt in the destination mailbox and check the spam folder.
5. Inspect message headers for SPF, DKIM, and DMARC results.
6. Confirm the visible `From` and SMTP return path align with the intended design.
7. Confirm that no credential value appears in logs, deployment manifests, shell history, or source control.
8. Send only a small number of controlled validation messages before increasing volume.

An SMTP `250` response means OCI accepted the message for processing. It does not prove that the recipient provider placed it in the inbox. Use relay logs and destination headers for end-to-end validation.

## 15. Default limits and capacity planning

Oracle documents the following default limits. The Console's **Limits, Quotas and Usage** page is authoritative for a particular tenancy.

| Resource | Enterprise / paid default | Trial default | Always Free default |
| --- | ---: | ---: | ---: |
| Emails in 24 hours | 50,000 | 200 | 0 |
| Approved senders | 10,000 | 2,000 | 10 |
| SMTP credentials per IAM user | 2 | 2 | 2 |
| Emails per minute | 18,000 | 10 | 10 |
| Encoded message size, including headers | 2 MB | 2 MB | 2 MB |

OCI counts each unique recipient and each 2 MB chunk toward limits. A message sent to ten recipients counts as ten emails. SPF and DKIM are required before Oracle considers a limit increase. Oracle states that approved message-size increases can reach up to 60 MB, subject to review.

## 16. Monitoring and reputation operations

Use the `oci_emaildelivery` metric namespace and the deliverability dashboard. Monitor at least:

- Emails accepted and relayed.
- Hard and soft bounces.
- Suppressed messages.
- Complaints and unsubscribes.
- Open and click counts when those signals are applicable and legally appropriate.

Operational practices:

- Alert on sudden increases in hard bounces, complaints, or suppressed recipients.
- Separate transactional and marketing mail streams when their risk and volume differ.
- Send only to recipients with the required consent.
- Increase volume gradually for a new domain or dedicated IP.
- Use retries with exponential backoff for transient SMTP failures. Do not retry permanent recipient failures.
- Keep the sending region near the application where practical.
- Use compartment quotas and IAM separation to limit accidental or unauthorized sending.

## 17. Suppression-list handling

OCI automatically suppresses recipients after hard bounces, complaints, manual entries, and list-unsubscribe requests. This mechanism cannot be disabled.

1. Search `OutboundAccepted` for `Recipient suppressed` when OCI accepts but blocks a recipient.
2. Search `OutboundRelayed` bounce events for the bounce category, SMTP status, and message.
3. Fix the source-data problem before deleting any suppression.
4. Never remove a suppression caused by a genuine spam complaint or a confirmed invalid mailbox merely to force another attempt.
5. For a distribution list, remove the invalid member first, then remove the list address from suppression only after verification.

## 18. Credential rotation

Because a user may have two SMTP credentials, use an overlap rotation:

1. Generate a second credential for the dedicated IAM user.
2. Store it as a new secret version.
3. Deploy the new credential to the application.
4. Send a controlled validation email.
5. Confirm accepted and relayed logs.
6. Delete the old SMTP credential in OCI.
7. Remove the old secret version from runtime systems according to the organization's retention policy.

## 19. Troubleshooting matrix

| Symptom | Likely cause | Checks and corrective action |
| --- | --- | --- |
| Connection timeout | Egress firewall, wrong endpoint, blocked port | Copy the endpoint again from Configuration; test TCP 465; inspect proxy and security rules |
| TLS handshake failure | TLS interception, old cipher support, wrong hostname | Keep certificate verification on; use the exact hostname; update the runtime; inspect corporate TLS proxies |
| `535` authentication failure | Wrong credential type, malformed copy, deleted credential | Use the generated SMTP username/password; rotate and retry; never use the Console password |
| Sender authorization failure | Missing IAM `use`, wrong region, or sender propagation | Check group membership and policy; create the sender in the endpoint's region; retry with backoff |
| `From` rejected | Address does not match approved sender or approved domain | Create the exact sender, or activate DKIM and create `@domain` |
| Recipient suppressed | Hard bounce, complaint, unsubscribe, or manual suppression | Inspect regional suppression list and logs; correct the cause before removal |
| Accepted but not received | Downstream bounce, spam filtering, or delay | Query `OutboundRelayed`; inspect destination spam folder and authentication headers |
| SPF fails | Wrong include, duplicate SPF records, DNS not propagated | Merge into one SPF TXT record and verify with an external resolver |
| DKIM inactive | Wrong CNAME name or target, DNS caching | Compare OCI values character by character and wait for TTL expiry |
| Rate or daily limit error | Tenancy limit exceeded | Check Limits, Quotas and Usage; reduce rate; request an increase after SPF and DKIM are active |
| Message-size error | MIME/base64 content exceeds the limit | Reduce body or attachments; remember that base64 and headers count toward encoded size |
| Attachment cannot be opened | Missing path or Linux permission failure | Use an absolute path and grant the runtime account read access without making the file world-readable |
| Attachment is not a regular file | Path identifies a directory, device, socket, or pipe | Export the content to a regular file before invoking the client |
| Duplicate attachment | Same absolute path appears more than once | Remove the duplicate from the environment or repeated flags |

Connectivity test for implicit TLS:

```bash
openssl s_client -connect smtp.email.eu-frankfurt-1.oci.oraclecloud.com:465 \
  -servername smtp.email.eu-frankfurt-1.oci.oraclecloud.com
```

## 20. Production readiness checklist

- [ ] Dedicated SMTP IAM user exists and has only required permissions.
- [ ] Credentials reside in a secret store and never in source control.
- [ ] Email domain exists in every sending region.
- [ ] `OutboundAccepted` and `OutboundRelayed` logs are enabled.
- [ ] One valid SPF record contains the correct OCI include.
- [ ] DKIM signing is active.
- [ ] Approved sender or approved domain exists in the endpoint's region.
- [ ] Application uses the Console-provided endpoint and encrypted SMTP.
- [ ] Test email passes SPF and DKIM checks.
- [ ] Suppression handling and recipient-consent processes are documented.
- [ ] Alerts cover bounces, complaints, suppressions, and unexpected volume.
- [ ] Retry logic distinguishes temporary from permanent failures.
- [ ] Attachment files are regular files, use absolute paths, and have least-privilege Linux ownership and permissions.
- [ ] Representative attachment messages pass dry-run validation within the active encoded-message limit.
- [ ] Credential rotation has an owner and schedule.
- [ ] The tenancy's actual limits support the planned volume and message size.

## Official Oracle references

- [Email Delivery overview](https://docs.oracle.com/en-us/iaas/Content/Email/Concepts/overview.htm)
- [Getting Started](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted.htm)
- [Creating user permissions](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-create-policy.htm)
- [Managing SMTP credentials](https://docs.oracle.com/en-us/iaas/Content/Identity/access/working-with-smtp-credentials.htm)
- [Creating an email domain](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-create-email-domain.htm)
- [Configuring SPF](https://docs.oracle.com/en-us/iaas/Content/Email/Tasks/configurespf.htm)
- [Setting up an email domain with DKIM](https://docs.oracle.com/en-us/iaas/Content/Email/Tasks/managing_dkim-setup_email_domain_with_dkim.htm)
- [Creating an approved sender](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-Create_an_approved_sender.htm)
- [Configuring the SMTP connection](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/gettingstarted_topic-Configure_the_SMTP_connection.htm)
- [Email Delivery IAM policy reference](https://docs.oracle.com/en-us/iaas/Content/Identity/policyreference/emailpolicyreference.htm)
- [Metrics and logs](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/guide-to-metrics-logs.htm)
- [Email log searching](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/log-guide.htm)
- [Managing the suppression list](https://docs.oracle.com/en-us/iaas/Content/Email/Tasks/managingsuppressionlist.htm)
- [Deliverability best practices](https://docs.oracle.com/en-us/iaas/Content/Email/Reference/deliverabilitybestpractices_topic-bestpractices.htm)
- [Limits by service](https://docs.oracle.com/en-us/iaas/Content/General/service-limits/default.htm)
