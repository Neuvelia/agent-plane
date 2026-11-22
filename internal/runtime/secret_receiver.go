package runtime

// SecretReceiver is an optional capability of an Executor: it states for which
// node types it hands assigned secrets to the node process. An executor that
// does not declare the capability would silently drop a secret, so preflight
// fails closed with secret_delivery_unsupported instead (F3.8, ADR-0029).
type SecretReceiver interface {
	ReceivesSecrets(nodeType string) bool
}
