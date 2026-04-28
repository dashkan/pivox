package apierr

// PostgreSQL SQLSTATE codes used by handlers to map driver errors to
// gRPC status codes. Values are from the SQL standard / Postgres error
// reference (https://www.postgresql.org/docs/current/errcodes-appendix.html);
// they are not Pivox-specific and do not change.
//
// Today only PgUniqueViolation is consumed by HandleResourceError.
// The others are declared so future handlers that need to distinguish
// FK / not-null / check / serialization failures have a single typed
// reference instead of inlining the magic string.
const (
	// PgUniqueViolation — class 23, unique_violation. Raised when an
	// INSERT or UPDATE would create a duplicate row in a column with
	// a UNIQUE constraint.
	PgUniqueViolation = "23505"

	// PgForeignKeyViolation — class 23, foreign_key_violation. Raised
	// when an INSERT/UPDATE would point at a non-existent parent row,
	// or when a DELETE would orphan child rows under a non-cascading FK.
	PgForeignKeyViolation = "23503"

	// PgNotNullViolation — class 23, not_null_violation. Raised when
	// an INSERT/UPDATE leaves a NOT NULL column null.
	PgNotNullViolation = "23502"

	// PgCheckViolation — class 23, check_violation. Raised when an
	// INSERT/UPDATE would violate a CHECK constraint on the table.
	PgCheckViolation = "23514"

	// PgSerializationFailure — class 40, serialization_failure. Raised
	// inside SERIALIZABLE transactions when concurrent updates conflict;
	// callers retry the whole transaction.
	PgSerializationFailure = "40001"
)
