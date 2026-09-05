#!/usr/bin/env bash
#
# MatrixCtrl installer.
#
# This exists because of a transcript. An operator installed onto a fresh VM and
# hit four failures in a row, none of them in MatrixCtrl:
#
#   1. `Kubernetes cluster unreachable: Get "http://localhost:8080/version"` —
#      KUBECONFIG was not set. k3s writes its kubeconfig to a root-only path and
#      Helm's error names a port instead of the cause.
#   2. `helm uninstall` printed "These resources were kept due to the resource
#      policy" and left the Postgres volume behind, so the reinstall inherited a
#      database and behaved like an upgrade.
#   3. The admin password was empty, because the release carrying it had failed
#      to publish and Helm resolved the previous chart.
#   4. A README command containing a literal "…" was pasted into a shell.
#
# Each of those is a step between "I have a server" and "I am logged in", and
# every one of them was left to the operator to get right. This script walks that
# path itself and reports what it finds, in words, at the point where it finds it.
#
# Usage:
#   ./install.sh                  interactive install or upgrade
#   ./install.sh install --host matrixctrl.example.com --tls letsencrypt --yes
#   ./install.sh uninstall        removes what `helm uninstall` leaves behind, after asking
#   ./install.sh password         prints the admin password
#   ./install.sh status           release, pods, ingress, URL
#
# It can be piped:  curl -fsSL <raw-url>/scripts/install.sh | bash
# Prompts are read from /dev/tty, so piping does not make the script answer itself.

set -euo pipefail

NAMESPACE="matrixctrl"
RELEASE="matrixctrl"
CHART="oci://ghcr.io/bxnnyg/charts/matrixctrl"
SECRET="matrixctrl-secret"

# ---------------------------------------------------------------- presentation

if [ -t 1 ]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; RED=$'\033[31m'; GREEN=$'\033[32m'
  YELLOW=$'\033[33m'; CYAN=$'\033[36m'; RESET=$'\033[0m'
else
  BOLD=""; DIM=""; RED=""; GREEN=""; YELLOW=""; CYAN=""; RESET=""
fi

say()  { printf '%s\n' "$*"; }
ok()   { printf '%s✓%s %s\n' "$GREEN" "$RESET" "$*"; }
warn() { printf '%s!%s %s\n' "$YELLOW" "$RESET" "$*"; }
info() { printf '%s·%s %s\n' "$DIM" "$RESET" "$*"; }
head_() { printf '\n%s%s%s\n' "$BOLD" "$*" "$RESET"; }

# Failure is the interesting path, so it gets more than an exit code: what was
# attempted, and the next thing to try.
die() {
  printf '\n%s✗ %s%s\n' "$RED" "$1" "$RESET" >&2
  shift
  for line in "$@"; do printf '  %s\n' "$line" >&2; done
  exit 1
}

# Prompts come from the terminal, not from stdin — stdin is the script itself
# when this runs as `curl … | bash`, and reading it would consume the source and
# answer every question with a line of shell.
TTY=""
if [ -r /dev/tty ]; then TTY=/dev/tty; fi

ASSUME_YES=0
DELETE_DATA=0
DRY_RUN=0

ask() { # ask <varname> <prompt> [default]
  local __var="$1" __prompt="$2" __default="${3:-}" __reply=""
  if [ -z "$TTY" ] || [ "$ASSUME_YES" = 1 ]; then
    [ -n "$__default" ] || die "no terminal to ask \"$__prompt\"" \
      "Run the script from a terminal, or pass every value as a flag (--help)."
    printf -v "$__var" '%s' "$__default"
    return
  fi
  if [ -n "$__default" ]; then
    printf '%s %s[%s]%s ' "$__prompt" "$DIM" "$__default" "$RESET" > "$TTY"
  else
    printf '%s ' "$__prompt" > "$TTY"
  fi
  IFS= read -r __reply < "$TTY" || true
  [ -n "$__reply" ] || __reply="$__default"
  printf -v "$__var" '%s' "$__reply"
}

confirm() { # confirm <question> <default y|n>
  local q="$1" def="${2:-n}" reply="" hint="y/N"
  [ "$def" = "y" ] && hint="Y/n"
  if [ "$ASSUME_YES" = 1 ]; then return 0; fi
  if [ -z "$TTY" ]; then [ "$def" = "y" ]; return; fi
  printf '%s %s[%s]%s ' "$q" "$DIM" "$hint" "$RESET" > "$TTY"
  IFS= read -r reply < "$TTY" || true
  [ -n "$reply" ] || reply="$def"
  case "$reply" in [yY]*) return 0 ;; *) return 1 ;; esac
}

# Deleting the volumes is the one irreversible thing here, so it is deliberately
# not covered by --yes. An unattended run that wanted to keep going would
# otherwise erase a database because a flag meant "stop asking me things".
# Destroying data takes a flag that says so, or a person at a terminal.
confirm_destructive() { # confirm_destructive <question>
  if [ "$DELETE_DATA" = 1 ]; then return 0; fi
  if [ -z "$TTY" ]; then
    info "keeping the existing data (pass --delete-data to remove it without asking)"
    return 1
  fi
  local reply=""
  printf '%s%s%s %s[type "delete" to confirm]%s ' "$YELLOW" "$1" "$RESET" "$DIM" "$RESET" > "$TTY"
  IFS= read -r reply < "$TTY" || true
  [ "$reply" = "delete" ]
}

# ------------------------------------------------------------------- preflight

need_tools() {
  local missing=0
  command -v kubectl >/dev/null 2>&1 || {
    warn "kubectl not found"
    say  "    k3s ships one:  ln -s /usr/local/bin/k3s /usr/local/bin/kubectl"
    say  "    otherwise:      https://kubernetes.io/docs/tasks/tools/"
    missing=1
  }
  command -v helm >/dev/null 2>&1 || {
    warn "helm not found"
    say  "    curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash"
    missing=1
  }
  [ "$missing" = 0 ] || die "Missing prerequisites." \
    "Install the tools above and run this again."
  ok "kubectl and helm are on PATH"
}

# The first failure in the transcript. k3s writes a root-only kubeconfig to a
# path nothing else looks at, so Helm falls back to localhost:8080 and reports a
# refused connection — true, and useless.
find_kubeconfig() {
  if [ -n "${KUBECONFIG:-}" ] && [ -r "$KUBECONFIG" ]; then
    ok "KUBECONFIG=$KUBECONFIG"
    return
  fi
  local candidate
  for candidate in "$HOME/.kube/config" /etc/rancher/k3s/k3s.yaml /etc/kubernetes/admin.conf; do
    if [ -r "$candidate" ]; then
      export KUBECONFIG="$candidate"
      ok "kubeconfig found at $candidate"
      [ "$candidate" = "$HOME/.kube/config" ] || \
        info "not the default location — export KUBECONFIG=$candidate in your shell to keep it"
      return
    fi
  done
  die "No kubeconfig found." \
    "Looked at: \$KUBECONFIG, ~/.kube/config, /etc/rancher/k3s/k3s.yaml, /etc/kubernetes/admin.conf" \
    "On k3s the file is root-only — run this as root, or copy it and chown it to your user."
}

check_cluster() {
  if kubectl get --raw='/version' --request-timeout=10s >/dev/null 2>&1; then
    local node_count
    node_count=$(kubectl get nodes --no-headers 2>/dev/null | wc -l | tr -d ' ')
    ok "cluster reachable ($node_count node(s))"
    return
  fi
  local hint="Check that your cluster is running and \$KUBECONFIG points at it."
  if command -v systemctl >/dev/null 2>&1 && systemctl list-unit-files k3s.service >/dev/null 2>&1; then
    hint="k3s is installed but the API did not answer — try: systemctl status k3s"
  fi
  die "Cluster unreachable via ${KUBECONFIG:-default kubeconfig}." "$hint"
}

# Not fatal. The chart creates an Ingress either way; without a controller it is
# an object nobody reads, and saying so now beats a hostname that never answers.
check_ingress_controller() {
  local classes
  classes=$(kubectl get ingressclass -o jsonpath='{range .items[*]}{.metadata.name}{" "}{end}' 2>/dev/null || true)
  if [ -n "$classes" ]; then
    ok "ingress controller present (IngressClass: ${classes% })"
  else
    warn "no IngressClass found — is an ingress controller installed?"
    info "k3s ships Traefik by default; without one the hostname will not resolve to the app"
  fi
}

CERT_ISSUERS=""
detect_cert_manager() {
  if ! kubectl get crd clusterissuers.cert-manager.io >/dev/null 2>&1; then
    info "cert-manager is not installed (that only rules out the Let's Encrypt option)"
    return
  fi
  CERT_ISSUERS=$(kubectl get clusterissuer -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
  if [ -n "$CERT_ISSUERS" ]; then
    ok "cert-manager present, ClusterIssuers: $(printf '%s' "$CERT_ISSUERS" | tr '\n' ' ')"
  else
    warn "cert-manager is installed but has no ClusterIssuer — Let's Encrypt cannot be used yet"
  fi
}

release_exists() { helm status "$RELEASE" -n "$NAMESPACE" >/dev/null 2>&1; }

# The second failure in the transcript. `helm uninstall` keeps the PVCs and the
# Secret on purpose (resource-policy: keep — losing a database to a typo is
# worse), and deleting the namespace does not necessarily remove a retained PV.
# The result is a fresh install on top of an old database: migrations are already
# applied, the bootstrap row already exists, and the install has nothing to say.
# That is a decision, so it gets asked rather than guessed.
leftovers_from_previous_install() {
  local found=""
  local pvc
  for pvc in matrixctrl-postgres matrixctrl-config; do
    if kubectl get pvc "$pvc" -n "$NAMESPACE" >/dev/null 2>&1; then
      found="$found pvc/$pvc"
    fi
  done
  if kubectl get secret "$SECRET" -n "$NAMESPACE" >/dev/null 2>&1; then
    found="$found secret/$SECRET"
  fi
  printf '%s' "${found# }"
}

delete_leftovers() {
  local pvc
  for pvc in matrixctrl-postgres matrixctrl-config; do
    kubectl delete pvc "$pvc" -n "$NAMESPACE" --ignore-not-found --timeout=120s >/dev/null 2>&1 || true
  done
  kubectl delete secret "$SECRET" -n "$NAMESPACE" --ignore-not-found >/dev/null 2>&1 || true
  # A PV with a Retain policy outlives its claim and comes back as Released,
  # where it is unusable but still counted. Only ones bound to this namespace.
  local pv
  for pv in $(kubectl get pv -o jsonpath='{range .items[?(@.spec.claimRef.namespace=="'"$NAMESPACE"'")]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true); do
    kubectl delete pv "$pv" --ignore-not-found --timeout=120s >/dev/null 2>&1 || true
  done
}

# ------------------------------------------------------------------- the modes

# What the four answers mean in chart values. Kept in one place because the
# combinations are not obvious and getting one wrong produces a site that
# half-works: TLS on an entrypoint that does not terminate it, or an ACME
# challenge that cannot reach the server.
tls_values() { # tls_values <mode> <issuer>
  case "$1" in
    letsencrypt)
      printf -- '--set ingress.entrypoint=websecure --set ingress.tls=true --set ingress.certIssuer=%s' "$2" ;;
    cloudflare-full)
      printf -- '--set ingress.entrypoint=websecure --set ingress.tls=true --set ingress.certIssuer=' ;;
    cloudflare-flexible|none)
      printf -- '--set ingress.entrypoint=web --set ingress.tls=false --set ingress.certIssuer=' ;;
    *) die "unknown TLS mode: $1" "Valid: letsencrypt, cloudflare-full, cloudflare-flexible, none" ;;
  esac
}

choose_tls_mode() {
  head_ "How should HTTPS be terminated?"
  say "  1) ${BOLD}letsencrypt${RESET}          cert-manager issues a real certificate."
  say "     ${DIM}Needs cert-manager + a ClusterIssuer, and port 80 reachable from the"
  say "     internet for the HTTP-01 challenge. Behind Cloudflare's proxy (orange"
  say "     cloud) HTTP-01 fails — grey-cloud the record, or use DNS-01, or pick 2.${RESET}"
  say "  2) ${BOLD}cloudflare-full${RESET}      Cloudflare proxies, SSL mode ${BOLD}Full${RESET}."
  say "     ${DIM}Traefik serves its own default certificate; Cloudflare accepts it and"
  say "     the browser sees Cloudflare's. Nothing to install, no ACME. Not 'Full"
  say "     (strict)' — that one verifies the origin certificate.${RESET}"
  say "  3) ${BOLD}cloudflare-flexible${RESET}  Cloudflare proxies, SSL mode ${BOLD}Flexible${RESET}."
  say "     ${DIM}The origin speaks plain HTTP. Simplest, and unencrypted between"
  say "     Cloudflare and your server.${RESET}"
  say "  4) ${BOLD}none${RESET}                 Plain HTTP. LAN, or a tunnel that terminates TLS."

  local default_choice=3 issuer_default=""
  if [ -n "$CERT_ISSUERS" ]; then
    default_choice=1
    issuer_default=$(printf '%s' "$CERT_ISSUERS" | grep -i 'prod' | head -1 || true)
    [ -n "$issuer_default" ] || issuer_default=$(printf '%s' "$CERT_ISSUERS" | head -1)
  fi

  local choice
  ask choice "Choice 1-4:" "$default_choice"
  case "$choice" in
    1|letsencrypt)
      TLS_MODE=letsencrypt
      [ -n "$CERT_ISSUERS" ] || warn "no ClusterIssuer detected — the certificate will stay pending until one exists"
      ask CERT_ISSUER "ClusterIssuer name:" "${issuer_default:-letsencrypt-prod}" ;;
    2|cloudflare-full)     TLS_MODE=cloudflare-full ;;
    3|cloudflare-flexible) TLS_MODE=cloudflare-flexible ;;
    4|none)                TLS_MODE=none ;;
    *) die "not a choice: $choice" "Pick 1, 2, 3 or 4." ;;
  esac
}

# ------------------------------------------------------------------ the result

# An install is finished when someone is logged in, not when Helm prints
# "deployed". Everything needed for that next step is printed here, once.
print_access_details() {
  local scheme="https" serves_tls
  serves_tls=$(kubectl -n "$NAMESPACE" get ingress matrixctrl \
    -o jsonpath='{.metadata.annotations.traefik\.ingress\.kubernetes\.io/router\.tls}' 2>/dev/null || true)
  case "$serves_tls" in
    true) scheme="https" ;;
    "")   case "$TLS_MODE" in cloudflare-flexible|none) scheme="http" ;; esac ;;
    *)    scheme="http" ;;
  esac

  local password
  password=$(read_password || true)

  head_ "MatrixCtrl is running"
  say "  URL       ${CYAN}${scheme}://${HOST}${RESET}"
  say "  User      ${BOLD}admin${RESET}"
  if [ -n "$password" ]; then
    say "  Password  ${BOLD}${password}${RESET}"
    info "kept in secret/$SECRET across upgrades — re-read it any time with: $0 password"
  else
    warn "no admin-password in secret/$SECRET"
    info "charts before 0.1.71 did not store one; upgrade and it will be generated"
  fi

  head_ "DNS"
  say "  ${HOST} must resolve to this node's public IP (an A record, or a CNAME)."
  if command -v getent >/dev/null 2>&1; then
    local resolved
    resolved=$(getent hosts "$HOST" 2>/dev/null | awk '{print $1}' | head -1 || true)
    if [ -n "$resolved" ]; then
      ok "$HOST currently resolves to $resolved"
    else
      warn "$HOST does not resolve yet — the browser will not reach it until it does"
    fi
  fi

  case "${TLS_MODE:-}" in
    letsencrypt)
      head_ "Certificate"
      say "  cert-manager issues it in the background. Watch it with:"
      say "    kubectl -n $NAMESPACE get certificate,order,challenge" ;;
    cloudflare-full|cloudflare-flexible)
      head_ "Cloudflare"
      say "  Set the SSL/TLS mode for this zone to ${BOLD}${TLS_MODE#cloudflare-}${RESET} and proxy the record (orange cloud)." ;;
  esac

  head_ "Next"
  say "  Open the URL, sign in, then go to ${BOLD}Setup${RESET} — MatrixCtrl finds your ESS"
  say "  (or deploys one) and can switch login over to Matrix accounts from there."
  say ""
}

read_password() {
  kubectl -n "$NAMESPACE" get secret "$SECRET" \
    -o jsonpath='{.data.admin-password}' 2>/dev/null | base64 -d 2>/dev/null
}

# ------------------------------------------------------------------ subcommands

cmd_install() {
  head_ "Checking this machine"
  need_tools
  find_kubeconfig
  check_cluster
  check_ingress_controller
  detect_cert_manager

  local upgrading=0
  if release_exists; then
    upgrading=1
    ok "existing release '$RELEASE' in namespace '$NAMESPACE' — this will upgrade it"
  else
    local leftovers
    leftovers=$(leftovers_from_previous_install)
    if [ -n "$leftovers" ]; then
      head_ "A previous installation left data behind"
      say "  $leftovers"
      say ""
      say "  ${DIM}\`helm uninstall\` keeps these on purpose, so an accidental uninstall does"
      say "  not delete your database. The catch: installing again on top of them is an"
      say "  upgrade wearing an install's clothes — the schema is migrated, the admin"
      say "  user already exists, and nothing about the install says so.${RESET}"
      say ""
      if confirm_destructive "Delete them and start genuinely fresh? This destroys the database."; then
        delete_leftovers
        ok "removed"
      else
        info "keeping them — your existing data and admin password carry over"
      fi
    fi
  fi

  if [ -z "$HOST" ]; then
    local current_host=""
    if [ "$upgrading" = 1 ]; then
      current_host=$(helm get values "$RELEASE" -n "$NAMESPACE" -o yaml 2>/dev/null \
        | awk '/^ingress:/{f=1;next} f&&/^ {2}host:/{print $2;exit} /^[a-z]/{f=0}' || true)
    fi
    head_ "Hostname"
    say "  ${DIM}The public name MatrixCtrl answers on, e.g. matrixctrl.example.com${RESET}"
    ask HOST "Hostname:" "$current_host"
    [ -n "$HOST" ] || die "A hostname is required." "Re-run with --host matrixctrl.example.com"
  fi
  case "$HOST" in
    *[!a-zA-Z0-9.-]*) die "Hostname contains characters a hostname cannot have: $HOST" \
      "If you copied this from documentation, check for a stray … or a quote." ;;
  esac

  if [ -z "$TLS_MODE" ]; then
    if [ "$upgrading" = 1 ]; then
      if confirm "Change how TLS is terminated? (currently unchanged)" n; then
        choose_tls_mode
      else
        info "keeping the current TLS settings"
      fi
    else
      choose_tls_mode
    fi
  fi

  # Values the operator supplied before are carried over explicitly rather than
  # with --reuse-values, which also freezes the *chart's* defaults at their old
  # version — the way an upgrade silently keeps shipping last release's settings.
  local prev_values=""
  if [ "$upgrading" = 1 ]; then
    prev_values=$(mktemp)
    # Contains whatever the operator set, OIDC client secret included.
    trap "rm -f '$prev_values'" EXIT INT TERM
    if helm get values "$RELEASE" -n "$NAMESPACE" -o yaml > "$prev_values" 2>/dev/null; then
      case "$(tr -d '[:space:]' < "$prev_values")" in null|"") : > "$prev_values" ;; esac
    else
      : > "$prev_values"
    fi
  fi

  head_ "Installing"
  local -a args=(upgrade --install "$RELEASE" "$CHART"
    --namespace "$NAMESPACE" --create-namespace
    --set "ingress.host=$HOST"
    --set "ess.namespace=$ESS_NAMESPACE"
    --wait --timeout 5m)
  [ "$DRY_RUN" = 0 ] || args+=(--dry-run=server)
  [ -z "$CHART_VERSION" ] || args+=(--version "$CHART_VERSION")
  [ -z "$prev_values" ] || [ ! -s "$prev_values" ] || args+=(-f "$prev_values")
  if [ -n "$TLS_MODE" ]; then
    # shellcheck disable=SC2046  # deliberate word splitting: tls_values emits flags
    args+=($(tls_values "$TLS_MODE" "${CERT_ISSUER:-}"))
  fi
  [ -z "$ADMIN_PASSWORD" ] || args+=(--set "secrets.adminPassword=$ADMIN_PASSWORD")

  say "${DIM}helm ${args[*]}${RESET}"
  say ""
  if ! helm "${args[@]}"; then
    [ -z "$prev_values" ] || rm -f "$prev_values"
    die "Helm failed." \
      "Look at what the pod says:  kubectl -n $NAMESPACE logs deploy/matrixctrl -c matrixctrl" \
      "and at why it is not starting: kubectl -n $NAMESPACE describe pod -l app=matrixctrl"
  fi
  [ -z "$prev_values" ] || rm -f "$prev_values"

  if [ "$DRY_RUN" = 1 ]; then
    ok "dry run — nothing was applied"
    return
  fi
  print_access_details
}

cmd_uninstall() {
  need_tools
  find_kubeconfig
  check_cluster

  if release_exists; then
    head_ "Removing the release"
    helm uninstall "$RELEASE" -n "$NAMESPACE" --wait || true
  else
    warn "no Helm release '$RELEASE' in namespace '$NAMESPACE'"
  fi

  local leftovers
  leftovers=$(leftovers_from_previous_install)
  if [ -z "$leftovers" ]; then
    ok "nothing left behind"
    return
  fi

  head_ "Kept by the resource policy"
  say "  $leftovers"
  say ""
  say "  ${DIM}These survive \`helm uninstall\` so that removing the app does not remove"
  say "  your data. They also make the next install silently behave like an upgrade,"
  say "  which is why this is asked here instead of being left for later.${RESET}"
  say ""
  if confirm_destructive "Delete them too? This destroys the database and the config repository."; then
    delete_leftovers
    ok "removed — the next install will be a real first install"
  else
    info "kept. Reinstalling later will pick up this data, including the admin password."
  fi
}

cmd_password() {
  need_tools >/dev/null
  find_kubeconfig >/dev/null
  local password
  password=$(read_password || true)
  if [ -n "$password" ]; then
    printf '%s\n' "$password"
    return
  fi
  die "No admin-password in secret/$SECRET." \
    "Charts before 0.1.71 did not store one. Upgrade, and it is generated and kept:" \
    "  $0 install" \
    "Or set one yourself:" \
    "  $0 install --admin-password 'your-password'"
}

cmd_status() {
  need_tools
  find_kubeconfig
  check_cluster
  head_ "Release"
  helm status "$RELEASE" -n "$NAMESPACE" 2>/dev/null | sed -n '1,8p' || warn "not installed"
  head_ "Pods"
  kubectl -n "$NAMESPACE" get pods -o wide 2>/dev/null || true
  head_ "Ingress"
  kubectl -n "$NAMESPACE" get ingress 2>/dev/null || true
  if kubectl get crd certificates.cert-manager.io >/dev/null 2>&1; then
    head_ "Certificate"
    kubectl -n "$NAMESPACE" get certificate 2>/dev/null || true
  fi
}

usage() {
  cat <<'USAGE'
MatrixCtrl installer

  install     (default) install or upgrade, asking for anything not given
  uninstall   remove the release and, after asking, the data it keeps
  password    print the admin password from the release Secret
  status      release, pods, ingress, certificate

Options for install:
  --host <name>              public hostname, e.g. matrixctrl.example.com
  --tls <mode>               letsencrypt | cloudflare-full | cloudflare-flexible | none
  --issuer <name>            cert-manager ClusterIssuer (with --tls letsencrypt)
  --admin-password <secret>  set (or reset) the admin password
  --ess-namespace <name>     namespace of the ESS release to manage (default: ess)
  --version <x.y.z>          chart version (default: newest published)
  --namespace <name>         where to install MatrixCtrl (default: matrixctrl)
  --yes                      never ask; requires the values above to be given
  --delete-data              also remove kept PVCs/Secret. Destroys the database.
                             Not implied by --yes, on purpose.
  --dry-run                  render everything, apply nothing

Examples:
  ./install.sh
  ./install.sh install --host matrixctrl.example.com --tls letsencrypt --issuer letsencrypt-prod --yes
  ./install.sh password
USAGE
}

# ------------------------------------------------------------------------ main

HOST=""
TLS_MODE=""
CERT_ISSUER=""
ADMIN_PASSWORD=""
CHART_VERSION=""
ESS_NAMESPACE="ess"

COMMAND="install"
case "${1:-}" in
  install|uninstall|password|status) COMMAND="$1"; shift ;;
  -h|--help|help) usage; exit 0 ;;
esac

while [ $# -gt 0 ]; do
  case "$1" in
    --host)           HOST="${2:-}"; shift 2 ;;
    --tls)            TLS_MODE="${2:-}"; shift 2 ;;
    --issuer)         CERT_ISSUER="${2:-}"; shift 2 ;;
    --admin-password) ADMIN_PASSWORD="${2:-}"; shift 2 ;;
    --ess-namespace)  ESS_NAMESPACE="${2:-}"; shift 2 ;;
    --version)        CHART_VERSION="${2:-}"; shift 2 ;;
    --namespace)      NAMESPACE="${2:-}"; shift 2 ;;
    --chart)          CHART="${2:-}"; shift 2 ;;
    --yes|-y)         ASSUME_YES=1; shift ;;
    --dry-run)        DRY_RUN=1; shift ;;
    --delete-data)    DELETE_DATA=1; shift ;;
    -h|--help)        usage; exit 0 ;;
    *) die "unknown option: $1" "Run '$0 --help' for the list." ;;
  esac
done

# --yes means "do not ask", which only works if the answers are already here.
if [ "$ASSUME_YES" = 1 ] && [ "$COMMAND" = "install" ]; then
  [ -n "$HOST" ] || die "--yes needs --host" "There is no sensible default for a public hostname."
  [ -n "$TLS_MODE" ] || die "--yes needs --tls" "One of: letsencrypt, cloudflare-full, cloudflare-flexible, none."
fi
# Fail on an unknown --tls now, not after the cluster checks have run.
if [ -n "$TLS_MODE" ]; then tls_values "$TLS_MODE" "${CERT_ISSUER:-x}" >/dev/null; fi

case "$COMMAND" in
  install)   cmd_install ;;
  uninstall) cmd_uninstall ;;
  password)  cmd_password ;;
  status)    cmd_status ;;
esac
