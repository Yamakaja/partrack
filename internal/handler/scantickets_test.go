package handler

import (
	"testing"
	"time"
)

func TestTicketStore_SingleUse(t *testing.T) {
	s := newTicketStore()
	raw, err := s.mint(ticketStream, "sess-1", streamTicketTTL)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if id, ok := s.consume(raw, ticketStream); !ok || id != "sess-1" {
		t.Fatalf("first consume: got (%q,%v), want (sess-1,true)", id, ok)
	}
	if _, ok := s.consume(raw, ticketStream); ok {
		t.Error("second consume succeeded; ticket must be single-use")
	}
}

func TestTicketStore_WrongKindAndExpiry(t *testing.T) {
	s := newTicketStore()

	raw, _ := s.mint(ticketPair, "sess-2", pairTicketTTL)
	if _, ok := s.consume(raw, ticketStream); ok {
		t.Error("consume with wrong kind succeeded")
	}
	// Wrong-kind consume still invalidates the ticket (defensive single-use).
	if _, ok := s.consume(raw, ticketPair); ok {
		t.Error("ticket reusable after a wrong-kind attempt")
	}

	expired, _ := s.mint(ticketStream, "sess-3", -time.Second)
	if _, ok := s.consume(expired, ticketStream); ok {
		t.Error("expired ticket consumed")
	}
}
