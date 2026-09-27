package maxbot

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const MaxWebhookBytes = 256 << 10

type Task struct {
	RecipientUserID int64    `json:"recipient_user_id"`
	EventKey        [32]byte `json:"-"`
	RecipientLookup [32]byte `json:"-"`
}

type Parser struct{ key [32]byte }

func NewParser(botToken string) (*Parser, error) {
	if botToken == "" {
		return nil, errors.New("bot token is required")
	}
	mac := hmac.New(sha256.New, []byte(botToken))
	_, _ = mac.Write([]byte("student-navigator:bot-inbox:v1"))
	var key [32]byte
	copy(key[:], mac.Sum(nil))
	return &Parser{key: key}, nil
}

type update struct {
	UpdateType string `json:"update_type"`
	Timestamp  int64  `json:"timestamp"`
	ChatID     int64  `json:"chat_id"`
	Payload    string `json:"payload"`
	User       struct {
		UserID int64 `json:"user_id"`
		IsBot  bool  `json:"is_bot"`
	} `json:"user"`
	Message struct {
		Sender struct {
			UserID int64 `json:"user_id"`
			IsBot  bool  `json:"is_bot"`
		} `json:"sender"`
		Recipient struct {
			ChatType string `json:"chat_type"`
		} `json:"recipient"`
		Body struct {
			Mid  string `json:"mid"`
			Text string `json:"text"`
		} `json:"body"`
	} `json:"message"`
}

// Parse accepts the documented MAX Update shape and keeps only the sender ID
// needed for one greeting. Unknown valid updates are acknowledged and ignored.
func (p *Parser) Parse(raw []byte) (Task, bool, error) {
	if len(raw) == 0 || len(raw) > MaxWebhookBytes {
		return Task{}, false, errors.New("invalid webhook size")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var event update
	if err := decoder.Decode(&event); err != nil {
		return Task{}, false, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Task{}, false, errors.New("trailing JSON")
	}
	if event.UpdateType == "" || event.Timestamp <= 0 {
		return Task{}, false, errors.New("missing update type or timestamp")
	}
	var recipient int64
	var stableID string
	switch event.UpdateType {
	case "bot_started":
		if event.User.UserID <= 0 || event.User.IsBot || event.ChatID <= 0 {
			return Task{}, false, nil
		}
		recipient = event.User.UserID
		stableID = event.Payload
	case "message_created":
		if event.Message.Sender.UserID <= 0 || event.Message.Sender.IsBot || event.Message.Recipient.ChatType != "dialog" || strings.TrimSpace(event.Message.Body.Text) != "/start" {
			return Task{}, false, nil
		}
		recipient = event.Message.Sender.UserID
		stableID = event.Message.Body.Mid
	default:
		return Task{}, false, nil
	}
	task := Task{RecipientUserID: recipient}
	mac := hmac.New(sha256.New, p.key[:])
	writeField(mac, []byte("recipient"))
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], uint64(recipient))
	writeField(mac, number[:])
	copy(task.RecipientLookup[:], mac.Sum(nil))
	mac = hmac.New(sha256.New, p.key[:])
	writeField(mac, []byte(event.UpdateType))
	binary.BigEndian.PutUint64(number[:], uint64(event.Timestamp))
	writeField(mac, number[:])
	binary.BigEndian.PutUint64(number[:], uint64(recipient))
	writeField(mac, number[:])
	binary.BigEndian.PutUint64(number[:], uint64(event.ChatID))
	writeField(mac, number[:])
	writeField(mac, []byte(stableID))
	copy(task.EventKey[:], mac.Sum(nil))
	return task, true, nil
}

func writeField(w io.Writer, value []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = w.Write(size[:])
	_, _ = w.Write(value)
}
