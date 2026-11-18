// Package secrets holds the pure grammar of secret references (F3.8, ADR-0029):
// the <provider>/<name> reference, secret and environment-variable names, and
// the ambient-bypass rule. It resolves nothing, reads no environment and holds
// no value; resolution belongs to the runtime pipeline and the providers.
package secrets
