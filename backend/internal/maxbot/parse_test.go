package maxbot

import (
	"bytes"
	"testing"
)

func TestParseAndDeduplicate(t *testing.T) {
	p, err := NewParser("test-token")
	if err != nil {
		t.Fatal(err)
	}
	start := []byte(`{"update_type":"bot_started","timestamp":1780000000000,"chat_id":77,"user":{"user_id":42},"payload":"abc"}`)
	a, ok, err := p.Parse(start)
	if err != nil || !ok || a.RecipientUserID != 42 {
		t.Fatalf("start: %+v %v %v", a, ok, err)
	}
	b, ok, err := p.Parse(start)
	if err != nil || !ok || a.EventKey != b.EventKey || a.RecipientLookup != b.RecipientLookup {
		t.Fatal("same event must have same keys")
	}
	message := []byte(`{"update_type":"message_created","timestamp":1780000000001,"message":{"sender":{"user_id":42},"recipient":{"chat_type":"dialog"},"body":{"mid":"mid-1","text":"/start"}}}`)
	c, ok, err := p.Parse(message)
	if err != nil || !ok || c.RecipientUserID != 42 || a.EventKey == c.EventKey || a.RecipientLookup != c.RecipientLookup {
		t.Fatalf("message: %+v %v %v", c, ok, err)
	}
	ignored := bytes.Replace(message, []byte(`"/start"`), []byte(`"hello"`), 1)
	if _, ok, err := p.Parse(ignored); err != nil || ok {
		t.Fatal("ordinary message must be ignored")
	}
	if _, _, err := p.Parse([]byte(`{"update_type":`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}
