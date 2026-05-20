// Package email provides the Sender interface used by feature code that needs
// to send transactional mail (invitations today, password resets eventually).
//
// Implementations:
//   - StubSender: logs the message; default for local dev / tests.
//   - GCPSender: posts to the GCP Cloud Email REST API using ADC.
package email

import "context"

// Sender is the abstraction every caller depends on. Implementations are safe
// for concurrent use.
type Sender interface {
	Send(ctx context.Context, to, subject, html, text string) error
}
