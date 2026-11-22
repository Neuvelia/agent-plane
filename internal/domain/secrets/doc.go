// Package secrets holds the pure grammar of secret references (F3.8, ADR-0029):
// the <provider>/<name> reference, secret and environment-variable names, and
// the ambient-bypass rule. It resolves nothing and reads no environment;
// resolution belongs to the runtime pipeline and the providers. Value is the one
// type that carries a resolved secret and keeps it out of formatting and
// serialization; ErrNotFound and ErrProviderUnavailable are the facts a provider
// reports.
package secrets
