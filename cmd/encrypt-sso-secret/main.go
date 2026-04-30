// encrypt-sso-secret encrypts a SsoConfig client_secret with the
// configured encryptor and writes it to sso_configs.client_secret_ciphertext.
//
// Why this exists: the dev SSO seed (scripts/seeds/dev_acme_sso.sql)
// writes the IdP client_secret as plaintext bytes — fine under
// dev-mode NoOpEncryptor (passthrough), broken under prod-mode KMS.
// Re-running the seed against a prod-mode-targeted DB would
// silently re-poison the column. This tool encrypts using whatever
// Encryptor is wired (NoOpEncryptor in dev builds, GoogleCloudKMSEncryptor
// in prod) so the row's bytes match what the running broker can
// decrypt.
//
// Production secret rotation should NOT use this tool — go through
// `Organizations.UpdateSsoConfig` instead, which bumps revision/etag,
// records updated_by, and triggers the Firebase Admin SDK side
// effect to also rotate the secret in Firebase's OIDC provider
// config. This tool bypasses all of that and is intentionally
// localhost-only as a safety guard.
//
// Usage:
//
//	go run ./cmd/encrypt-sso-secret \
//	    --provider-id=oidc.acme \
//	    --secret="<plaintext>"
//
// `--database-url` and `PIVOX_GCP_KMS_KEY_NAME` follow the same env
// fallback chain as `pivox-cloud serve`.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dashkan/pivox/internal/crypto"
)

func main() {
	dbURL := flag.String("database-url", envOrDefault("PIVOX_DATABASE_URL", "postgres://localhost:5432/pivox?sslmode=disable"), "PostgreSQL connection URL")
	providerID := flag.String("provider-id", "", "firebase_provider_id of the SsoConfig row (e.g. oidc.acme)")
	secret := flag.String("secret", "", "Plaintext client_secret to encrypt and store")
	allowNonLocalhost := flag.Bool("allow-non-localhost", false, "Permit running against a non-localhost database. Off by default — production rotations should use Organizations.UpdateSsoConfig, which bumps revision/etag and syncs Firebase.")
	flag.Parse()

	if *providerID == "" || *secret == "" {
		flag.Usage()
		os.Exit(2)
	}

	if !*allowNonLocalhost {
		if err := assertLocalhost(*dbURL); err != nil {
			fmt.Fprintln(os.Stderr, "refusing to run:", err)
			fmt.Fprintln(os.Stderr, "  pass --allow-non-localhost to override (intended for staging/recovery only)")
			os.Exit(2)
		}
	}

	enc, err := crypto.NewEncryptor()
	if err != nil {
		fmt.Fprintln(os.Stderr, "init encryptor:", err)
		os.Exit(1)
	}
	ct, err := enc.Encrypt([]byte(*secret))
	if err != nil {
		fmt.Fprintln(os.Stderr, "encrypt:", err)
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect db:", err)
		os.Exit(1)
	}
	defer pool.Close()

	tag, err := pool.Exec(ctx,
		`UPDATE sso_configs
		    SET client_secret_ciphertext = $1, update_time = now()
		  WHERE firebase_provider_id = $2`,
		ct, *providerID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		os.Exit(1)
	}
	if tag.RowsAffected() == 0 {
		fmt.Fprintf(os.Stderr, "no row matched firebase_provider_id=%q\n", *providerID)
		os.Exit(1)
	}
	fmt.Printf("encrypted %d bytes of ciphertext into sso_configs (provider=%s)\n",
		len(ct), *providerID)
}

// assertLocalhost rejects non-loopback DB hosts. Catches the
// copy-pasted-prod-URL footgun: running against `pivox-prod-db.…`
// would silently rotate the production client_secret without any
// audit trail or Firebase-side sync.
func assertLocalhost(dbURL string) error {
	u, err := url.Parse(dbURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	host := u.Hostname()
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil
	}
	if strings.HasPrefix(host, "127.") {
		return nil
	}
	return fmt.Errorf("database host %q is not localhost; this tool is dev-only", host)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
