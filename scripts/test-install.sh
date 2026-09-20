#!/usr/bin/env bash
# Tests for the pure parts of install.sh.
#
# The installer is the first thing an operator runs and the only thing they run without
# a UI in front of it, and until now it had no tests at all. Two of its defects reached
# production: a consent prompt that answered itself (§4.86) and an image tag carried
# forward through five chart upgrades (§4.103). Both were testable in a second.
#
# This sources install.sh with MATRIXCTRL_SOURCE_ONLY=1, so it exercises the shipped
# functions rather than a copy — a copy is exactly what went wrong last time.
set -u

export MATRIXCTRL_SOURCE_ONLY=1
# shellcheck source=install.sh disable=SC1091
. "$(cd "$(dirname "$0")" && pwd)/install.sh"

fails=0
pass=0
check() { # check <name> <expected> <actual>
  if [ "$2" = "$3" ]; then
    pass=$((pass + 1))
  else
    printf 'FAIL  %s\n  expected:\n%s\n  actual:\n%s\n' "$1" "$2" "$3" >&2
    fails=$((fails + 1))
  fi
}

# helm is replaced by a function, which bash prefers over the binary on PATH.
FIXTURE=""
HELM_FAILS=0
helm() {
  [ "$HELM_FAILS" = 0 ] || return 1
  printf '%s' "$FIXTURE"
}

carried() { # carried <fixture>  -> the file carry_values would hand to -f
  FIXTURE="$1"
  local f out
  f=$(mktemp)
  carry_values "$f" >/dev/null 2>&1 || true
  out=$(cat "$f")
  rm -f "$f"
  printf '%s' "$out"
}

# ---------------------------------------------------------------- carry_values

# The reported case, verbatim from the live release.
check "the pin is removed, everything else survives" \
"$(printf 'ess:\n  namespace: ess\nimage:\n  pullPolicy: IfNotPresent\ningress:\n  host: example.org')" \
"$(carried "$(printf 'ess:\n  namespace: ess\nimage:\n  pullPolicy: IfNotPresent\n  tag: 0.1.70\ningress:\n  host: example.org\n')")"

# Removing the only key under image: must remove image: too — a bare `image:` is YAML
# null, which would blow up every template reading .Values.image.repository.
check "an image block left empty is dropped entirely" \
"$(printf 'ess:\n  namespace: ess\ningress:\n  host: example.org')" \
"$(carried "$(printf 'ess:\n  namespace: ess\nimage:\n  tag: 0.1.70\ningress:\n  host: example.org\n')")"

check "image: last in the file and left empty is dropped" \
"$(printf 'ess:\n  namespace: ess')" \
"$(carried "$(printf 'ess:\n  namespace: ess\nimage:\n  tag: 0.1.70\n')")"

check "image: last in the file keeps what remains" \
"$(printf 'ess:\n  namespace: ess\nimage:\n  repository: registry.internal/matrixctrl')" \
"$(carried "$(printf 'ess:\n  namespace: ess\nimage:\n  repository: registry.internal/matrixctrl\n  tag: 0.1.70\n')")"

check "a private mirror is instance config and stays" \
"$(printf 'image:\n  repository: registry.internal/matrixctrl\n  pullPolicy: Always')" \
"$(carried "$(printf 'image:\n  repository: registry.internal/matrixctrl\n  tag: 0.1.70\n  pullPolicy: Always\n')")"

check "no image block at all is left alone" \
"$(printf 'oidc:\n  enabled: true\n  issuer: https://example.org')" \
"$(carried "$(printf 'oidc:\n  enabled: true\n  issuer: https://example.org\n')")"

# A `tag:` belonging to something else must not be touched. Only the chart owns
# image.tag; a tag under any other key is the operator's business.
check "a tag under another key is not touched" \
"$(printf 'sfu:\n  tag: v1.2.3\nimage:\n  pullPolicy: IfNotPresent')" \
"$(carried "$(printf 'sfu:\n  tag: v1.2.3\nimage:\n  pullPolicy: IfNotPresent\n  tag: 0.1.70\n')")"

check "helm's literal null becomes nothing, not the string null" "" "$(carried "null
")"

check "no values at all is empty, not an error" "" "$(carried "")"

HELM_FAILS=1
check "an unreachable helm yields no values rather than junk" "" "$(carried "irrelevant")"
HELM_FAILS=0

# carry_values signals emptiness through its exit status, because every caller
# guards the -f flag on it and an empty -f file makes helm refuse.
# install.sh runs under `set -e`, which this script inherits by sourcing it, so the
# status has to be captured rather than read after the fact.
rc_of() { FIXTURE="$1"; local f rc=0; f=$(mktemp); carry_values "$f" >/dev/null 2>&1 || rc=$?; rm -f "$f"; printf '%s' "$rc"; }
check "empty values report non-zero" "1" "$(rc_of "")"
check "present values report zero" "0" "$(rc_of "$(printf 'ess:\n  namespace: ess\n')")"

# ---------------------------------------------------------------- sanitise

# From the transcript where a pasted hostname was rejected while being printed back
# unchanged: bracketed paste wraps it in escape sequences nobody can see (§4.77).
check "bracketed paste is stripped" "matrix.example.org" \
  "$(sanitise "$(printf '\033[200~matrix.example.org\033[201~')")"
check "a trailing carriage return is stripped" "matrix.example.org" \
  "$(sanitise "$(printf 'matrix.example.org\r')")"
check "surrounding whitespace is stripped" "matrix.example.org" \
  "$(sanitise "   matrix.example.org  ")"
check "inner text is left alone" "a b" "$(sanitise "a b")"

# ---------------------------------------------------------------- tls_values

check "letsencrypt passes the issuer through" \
  "--set ingress.entrypoint=websecure --set ingress.tls=true --set ingress.certIssuer=letsencrypt-prod" \
  "$(tls_values letsencrypt letsencrypt-prod)"
check "plain http turns tls off" \
  "--set ingress.entrypoint=web --set ingress.tls=false --set ingress.certIssuer=" \
  "$(tls_values cloudflare-flexible '')"

# ----------------------------------------------------------------

if [ "$fails" -gt 0 ]; then
  printf '\n%d of %d install.sh checks failed\n' "$fails" "$((pass + fails))" >&2
  exit 1
fi
printf '%d install.sh checks passed\n' "$pass"
