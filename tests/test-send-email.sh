#!/usr/bin/env bash

set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
script="$repository_root/scripts/send-email.sh"

output=$(
  OCI_EMAIL_FROM='no-reply@example.com' \
  OCI_EMAIL_TO='first@example.net, second@example.net' \
  OCI_EMAIL_CC='audit@example.net' \
  OCI_EMAIL_SUBJECT='Bash dry run' \
  OCI_EMAIL_TEXT=$'first line\nsecond line' \
  bash "$script" --dry-run
)

grep -Fq 'From: <no-reply@example.com>' <<< "$output"
grep -Fq 'To: first@example.net, second@example.net' <<< "$output"
grep -Fq 'Cc: audit@example.net' <<< "$output"
grep -Fq 'Subject: Bash dry run' <<< "$output"
grep -Fq 'Content-Type: text/plain; charset=UTF-8' <<< "$output"
grep -Fq 'first line' <<< "$output"
grep -Fq 'second line' <<< "$output"

if OCI_EMAIL_FROM='no-reply@example.com' \
  OCI_EMAIL_TO='recipient@example.net' \
  OCI_EMAIL_SUBJECT=$'safe\nBcc: injected@example.net' \
  OCI_EMAIL_TEXT='test' \
  bash "$script" --dry-run >/dev/null 2>&1; then
  echo 'expected header-injection input to be rejected' >&2
  exit 1
fi

if OCI_EMAIL_FROM='no-reply@example.com' \
  OCI_EMAIL_TO='not-an-address' \
  OCI_EMAIL_TEXT='test' \
  bash "$script" --dry-run >/dev/null 2>&1; then
  echo 'expected an invalid recipient to be rejected' >&2
  exit 1
fi

echo 'Bash email dry-run tests passed.'
