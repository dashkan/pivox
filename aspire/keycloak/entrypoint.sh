#!/usr/bin/env bash
#
# Keycloak entrypoint wrapper: trust extra CA/leaf certificates, then exec kc.sh.
#
# WHY THIS EXISTS (and why KC_TRUSTSTORE_PATHS is not enough).
#
# Keycloak's own --truststore-paths / KC_TRUSTSTORE_PATHS does NOT make the
# identity-broker's HTTP client trust the Aspire developer certificate. Two things
# defeat it, both visible with
# `KC_LOG_LEVEL=INFO,org.keycloak.truststore:trace`:
#
#   1. ORDERING. FileTruststoreProviderFactory — the truststore KC's HTTP client
#      actually uses — initializes from the JVM's cacerts BEFORE TruststoreBuilder
#      ever looks at the truststore paths.
#   2. CA-ONLY. Everything it ingests is logged "detected as root CA". The Aspire
#      dev cert is a self-signed LEAF (basicConstraints CA:FALSE), so it is never
#      added, and the cert never appears in the trace at all.
#
# The result is an SSO backchannel that dies with
#   javax.net.ssl.SSLHandshakeException: PKIX path building failed:
#   unable to find valid certification path to requested target
# — an error that says nothing about truststore configuration.
#
# So the cert has to be in the JVM truststore itself. cacerts is root-owned and
# read-only while Keycloak runs as uid 1000, so we copy it (keeping all ~146 public
# roots, which is what keeps Google/GitHub brokering working), append the extra
# certs, and point the JVM at the copy.
#
# No-op unless PIVOX_EXTRA_CA_DIR names a directory with *.pem in it — production
# images pass nothing and get stock behaviour.
set -euo pipefail

ca_dir="${PIVOX_EXTRA_CA_DIR:-}"

if [ -n "$ca_dir" ] && compgen -G "$ca_dir/*.pem" >/dev/null; then
  src="${JAVA_HOME:-/usr/lib/jvm/jre}/lib/security/cacerts"
  dest=/tmp/pivox-cacerts.p12

  if [ ! -r "$src" ]; then
    echo "entrypoint: cannot read the JVM truststore at $src" >&2
    exit 1
  fi

  cp "$src" "$dest"
  chmod u+w "$dest"

  # Alias per file. `-noprompt` so an already-present cert doesn't block startup.
  i=0
  for pem in "$ca_dir"/*.pem; do
    keytool -importcert -noprompt \
      -alias "pivox-extra-$i" \
      -file "$pem" \
      -keystore "$dest" \
      -storetype PKCS12 \
      -storepass changeit >/dev/null
    echo "entrypoint: trusted $(basename "$pem")"
    i=$((i + 1))
  done

  # JAVA_OPTS_APPEND is Keycloak's documented hook for extra JVM flags; kc.sh
  # appends it to the java command line.
  export JAVA_OPTS_APPEND="${JAVA_OPTS_APPEND:-} -Djavax.net.ssl.trustStore=$dest -Djavax.net.ssl.trustStorePassword=changeit"
fi

exec /opt/keycloak/bin/kc.sh "$@"
