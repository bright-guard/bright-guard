package email

import (
	"context"
	"log"
	"sync"
)

// StubSender records every Send call so tests can inspect, and logs a short
// summary so a local dev can verify their flow without configuring a real
// mail provider.
type StubSender struct {
	From string

	mu   sync.Mutex
	Sent []StubMessage
}

// StubMessage is one recorded Send; exposed for tests.
type StubMessage struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

func (s *StubSender) Send(_ context.Context, to, subject, html, text string) error {
	s.mu.Lock()
	s.Sent = append(s.Sent, StubMessage{To: to, Subject: subject, HTML: html, Text: text})
	s.mu.Unlock()

	preview := text
	if len(preview) > 200 {
		preview = preview[:200] + "…"
	}
	log.Printf("email (stub) to=%s subject=%q body=%q", to, subject, preview)
	return nil
}

// Messages returns a copy of the recorded messages.
func (s *StubSender) Messages() []StubMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]StubMessage, len(s.Sent))
	copy(out, s.Sent)
	return out
}
