#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/send-email.sh [--dry-run]

Send one plain-text email through OCI Email Delivery using curl and implicit
TLS on port 465. Configuration is read from OCI_SMTP_* and OCI_EMAIL_*
environment variables. Use --dry-run to print the MIME message without
connecting or requiring SMTP credentials.
EOF
}

fail() {
  printf 'send-email.sh: %s\n' "$*" >&2
  exit 1
}

reject_line_breaks() {
  local name=$1
  local value=$2
  if [[ $value == *$'\r'* || $value == *$'\n'* ]]; then
    fail "$name must not contain CR or LF characters"
  fi
}

trim() {
  local value=$1
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

validate_address() {
  local label=$1
  local address=$2
  if [[ ! $address =~ ^[^[:space:]\<\>\@,]+@[^[:space:]\<\>\@,]+$ ]]; then
    fail "$label contains an invalid bare email address: $address"
  fi
}

TO_ADDRESSES=()
CC_ADDRESSES=()
RECIPIENTS=()

parse_recipient_list() {
  local label=$1
  local raw=$2
  local required=$3
  local part address
  local parsed=0
  local -a parts=()

  reject_line_breaks "$label" "$raw"
  IFS=',' read -r -a parts <<< "$raw"
  for part in "${parts[@]}"; do
    address=$(trim "$part")
    [[ -n $address ]] || continue
    validate_address "$label" "$address"
    RECIPIENTS+=("$address")
    if [[ $label == OCI_EMAIL_TO ]]; then
      TO_ADDRESSES+=("$address")
    else
      CC_ADDRESSES+=("$address")
    fi
    parsed=$((parsed + 1))
  done

  if [[ $required == true && $parsed -eq 0 ]]; then
    fail "$label must contain at least one address"
  fi
}

join_addresses() {
  local result=''
  local address
  for address in "$@"; do
    if [[ -n $result ]]; then
      result+=', '
    fi
    result+="$address"
  done
  printf '%s' "$result"
}

write_body() {
  local body=$1
  local line
  body=${body//$'\r\n'/$'\n'}
  body=${body//$'\r'/$'\n'}
  while IFS= read -r line || [[ -n $line ]]; do
    printf '%s\r\n' "$line"
  done <<< "$body"
}

build_message() {
  printf 'Date: %s\r\n' "$(date -R)"
  printf 'From: <%s>\r\n' "$email_from"
  printf 'To: %s\r\n' "$(join_addresses "${TO_ADDRESSES[@]}")"
  if [[ ${#CC_ADDRESSES[@]} -gt 0 ]]; then
    printf 'Cc: %s\r\n' "$(join_addresses "${CC_ADDRESSES[@]}")"
  fi
  printf 'Subject: %s\r\n' "$email_subject"
  printf 'MIME-Version: 1.0\r\n'
  printf 'Content-Type: text/plain; charset=UTF-8\r\n'
  printf 'Content-Transfer-Encoding: 8bit\r\n'
  printf 'X-Mailer: oci-email-delivery-toolkit Bash/curl\r\n'
  printf '\r\n'
  write_body "$email_text"
}

curl_config_escape() {
  local value=$1
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  printf '%s' "$value"
}

dry_run=false
while [[ $# -gt 0 ]]; do
  case $1 in
    --dry-run)
      dry_run=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      fail "unknown argument: $1"
      ;;
  esac
  shift
done

email_from=${OCI_EMAIL_FROM:-}
email_to=${OCI_EMAIL_TO:-}
email_cc=${OCI_EMAIL_CC:-}
email_subject=${OCI_EMAIL_SUBJECT:-OCI Email Delivery test}
email_text=${OCI_EMAIL_TEXT:-}

[[ -n $email_from ]] || fail 'OCI_EMAIL_FROM is required'
validate_address OCI_EMAIL_FROM "$email_from"
reject_line_breaks OCI_EMAIL_FROM "$email_from"
reject_line_breaks OCI_EMAIL_SUBJECT "$email_subject"
[[ -n $email_text ]] || fail 'OCI_EMAIL_TEXT is required for the Bash client'

parse_recipient_list OCI_EMAIL_TO "$email_to" true
parse_recipient_list OCI_EMAIL_CC "$email_cc" false

if [[ $dry_run == true ]]; then
  build_message
  exit 0
fi

command -v curl >/dev/null 2>&1 || fail 'curl is required'

smtp_host=${OCI_SMTP_HOST:-}
smtp_port=${OCI_SMTP_PORT:-465}
smtp_username=${OCI_SMTP_USERNAME:-}
smtp_password=${OCI_SMTP_PASSWORD:-}
connect_timeout=${OCI_CURL_CONNECT_TIMEOUT:-30}

[[ $smtp_host =~ ^[A-Za-z0-9.-]+$ ]] || fail 'OCI_SMTP_HOST must be a bare DNS hostname'
[[ $smtp_port =~ ^[0-9]+$ && $smtp_port -ge 1 && $smtp_port -le 65535 ]] ||
  fail 'OCI_SMTP_PORT must be a number between 1 and 65535'
[[ -n $smtp_username && -n $smtp_password ]] ||
  fail 'OCI_SMTP_USERNAME and OCI_SMTP_PASSWORD are required'
[[ $smtp_username != *:* ]] || fail 'OCI_SMTP_USERNAME must not contain a colon'
reject_line_breaks OCI_SMTP_USERNAME "$smtp_username"
reject_line_breaks OCI_SMTP_PASSWORD "$smtp_password"
[[ $connect_timeout =~ ^[0-9]+$ && $connect_timeout -gt 0 ]] ||
  fail 'OCI_CURL_CONNECT_TIMEOUT must be a positive integer number of seconds'

credential_file=''
cleanup() {
  if [[ -n $credential_file && -f $credential_file ]]; then
    rm -f -- "$credential_file"
  fi
}
trap cleanup EXIT HUP INT TERM

umask 077
credential_file=$(mktemp "${TMPDIR:-/tmp}/oci-email-curl.XXXXXX")
printf 'user = "%s:%s"\n' \
  "$(curl_config_escape "$smtp_username")" \
  "$(curl_config_escape "$smtp_password")" > "$credential_file"

curl_args=(
  --silent
  --show-error
  --fail
  --config "$credential_file"
  --url "smtps://${smtp_host}:${smtp_port}"
  --ssl-reqd
  --tlsv1.2
  --connect-timeout "$connect_timeout"
  --mail-from "$email_from"
)

for recipient in "${RECIPIENTS[@]}"; do
  curl_args+=(--mail-rcpt "$recipient")
done

build_message | curl "${curl_args[@]}" --upload-file -
printf 'Email accepted by OCI Email Delivery for %d recipient(s).\n' "${#RECIPIENTS[@]}"
