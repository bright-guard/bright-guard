package email

import (
	"context"
	"strings"
	"testing"
)

func TestStubSenderRecordsCalls(t *testing.T) {
	s := &StubSender{From: "noreply@example.test"}
	if err := s.Send(context.Background(), "you@example.test", "hi", "<p>hi</p>", "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msgs := s.Messages()
	if len(msgs) != 1 || msgs[0].To != "you@example.test" || msgs[0].Subject != "hi" {
		t.Fatalf("recorded msg: %+v", msgs)
	}
	if !strings.Contains(msgs[0].HTML, "<p>") {
		t.Errorf("html lost: %q", msgs[0].HTML)
	}
}
