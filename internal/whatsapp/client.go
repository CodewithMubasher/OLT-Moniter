package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// PairDisplayName MUST be formatted "Browser (OS)" using a common browser/OS.
// WhatsApp validates it server-side and answers "400 bad-request" otherwise.
const PairDisplayName = "Chrome (Windows)"

const (
	maxSentIDs      = 512
	eventBufferSize = 256
)

type Client struct {
	WAClient  *whatsmeow.Client
	EventChan chan any

	store   *sqlstore.Container
	dataDir string

	mu          sync.Mutex
	sentMsgIDs  map[string]struct{}
	sentOrder   []string
	targetPhone string

	closeOnce sync.Once
	done      chan struct{}
}

func NewClient(dataDir string) *Client {
	return &Client{
		dataDir:    dataDir,
		EventChan:  make(chan any, eventBufferSize),
		sentMsgIDs: make(map[string]struct{}),
		done:       make(chan struct{}),
	}
}

// SetTarget sets the phone number (digits, with or without '+') whose messages we watch.
func (c *Client) SetTarget(phone string) {
	c.mu.Lock()
	c.targetPhone = DigitsOnly(phone)
	c.mu.Unlock()
}

func (c *Client) Target() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.targetPhone
}

func (c *Client) Initialize(ctx context.Context) error {
	if err := os.MkdirAll(c.dataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	dbPath := filepath.ToSlash(filepath.Join(c.dataDir, "whatsapp.db"))
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "journal_mode(WAL)")
	dsn := "file:" + dbPath + "?" + q.Encode()

	var err error
	c.store, err = sqlstore.New(ctx, "sqlite", dsn, waLog.Noop)
	if err != nil {
		return fmt.Errorf("open session database: %w", err)
	}

	device, err := c.store.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("load device: %w", err)
	}

	// Log to Noop: writing to stdout would corrupt the TUI.
	c.WAClient = whatsmeow.NewClient(device, waLog.Noop)
	c.WAClient.EnableAutoReconnect = true
	c.WAClient.InitialAutoReconnect = true
	c.WAClient.AutoTrustIdentity = true
	c.WAClient.AddEventHandler(c.handleEvent)
	return nil
}

// emit never blocks the whatsmeow socket goroutine indefinitely. Critical events wait
// for buffer space (or shutdown); non-critical ones are dropped if the UI is behind.
func (c *Client) emit(evt any, critical bool) {
	if critical {
		select {
		case c.EventChan <- evt:
		case <-c.done:
		}
		return
	}
	select {
	case c.EventChan <- evt:
	default:
	}
}

func (c *Client) handleEvent(raw any) {
	switch v := raw.(type) {
	case *events.QR:
		if len(v.Codes) > 0 {
			c.emit(QRCodeEvent{Code: v.Codes[0]}, false)
		}
	case *events.PairSuccess:
		c.emit(PairSuccessEvent{JID: v.ID.User}, true)
	case *events.PairError:
		c.emit(PairErrorEvent{Err: v.Error}, true)
	case *events.Connected:
		c.emit(ConnectedEvent{}, true)
	case *events.Disconnected:
		c.emit(DisconnectedEvent{}, false)
	case *events.LoggedOut:
		c.emit(LoggedOutEvent{Reason: v.Reason.String()}, true)
	case *events.StreamReplaced:
		c.emit(FatalEvent{Err: errors.New("session opened elsewhere (another NightCode instance or WhatsApp Web is using this login)")}, true)
	case *events.ClientOutdated:
		c.emit(FatalEvent{Err: errors.New("WhatsApp rejected this client version as outdated; run `go get -u go.mau.fi/whatsmeow@latest` and rebuild")}, true)
	case *events.TemporaryBan:
		c.emit(FatalEvent{Err: fmt.Errorf("account temporarily banned: %s", v.String())}, true)
	case *events.ConnectFailure:
		c.emit(FatalEvent{Err: fmt.Errorf("connection refused by WhatsApp: %s (code %d)", v.Reason.String(), int(v.Reason))}, true)
	case *events.Message:
		c.handleIncomingMessage(v)
	}
}

// markSent remembers an ID we sent so we never react to our own echo.
func (c *Client) markSent(id string) {
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.sentMsgIDs[id]; ok {
		return
	}
	c.sentMsgIDs[id] = struct{}{}
	c.sentOrder = append(c.sentOrder, id)
	if len(c.sentOrder) > maxSentIDs {
		drop := c.sentOrder[0]
		c.sentOrder = c.sentOrder[1:]
		delete(c.sentMsgIDs, drop)
	}
}

func (c *Client) wasSent(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.sentMsgIDs[id]
	return ok
}

// resolveSenderPhone returns the sender's phone digits, handling modern LID addressing.
func (c *Client) resolveSenderPhone(info types.MessageInfo) string {
	if info.SenderAlt.Server == types.DefaultUserServer && info.SenderAlt.User != "" {
		return info.SenderAlt.User
	}
	sender := info.Sender
	if sender.Server == types.DefaultUserServer {
		return sender.User
	}
	if sender.Server == types.HiddenUserServer && c.WAClient != nil && c.WAClient.Store != nil && c.WAClient.Store.LIDs != nil {
		pn, err := c.WAClient.Store.LIDs.GetPNForLID(context.Background(), sender.ToNonAD())
		if err == nil && !pn.IsEmpty() {
			return pn.User
		}
	}
	return ""
}

func (c *Client) handleIncomingMessage(msg *events.Message) {
	if msg.Info.IsFromMe {
		c.markSent(msg.Info.ID)
		return
	}
	if c.wasSent(msg.Info.ID) || msg.Info.IsGroup {
		return
	}

	target := c.Target()
	if target == "" {
		return
	}
	sender := c.resolveSenderPhone(msg.Info)
	if sender == "" || sender != target {
		return
	}

	text := ""
	if m := msg.Message; m != nil {
		text = m.GetConversation()
		if text == "" && m.GetExtendedTextMessage() != nil {
			text = m.GetExtendedTextMessage().GetText()
		}
	}
	if text == "" {
		text = "[non-text message]"
	}

	c.emit(MessageReceivedEvent{
		Sender:    "+" + sender,
		ReplyTo:   msg.Info.Chat,
		Text:      text,
		MsgID:     msg.Info.ID,
		Timestamp: msg.Info.Timestamp,
	}, true)
}

func (c *Client) IsLoggedIn() bool {
	return c.WAClient != nil && c.WAClient.Store != nil && c.WAClient.Store.ID != nil
}

// HasSavedSession checks whether the on-disk database contains device
// credentials from a previous linking. Returns true even if the session
// has since been invalidated by WhatsApp server-side — the app should
// still try to reconnect first rather than immediately asking for re-link.
func (c *Client) HasSavedSession(ctx context.Context) bool {
	if c.store == nil {
		return false
	}
	dev, err := c.store.GetFirstDevice(ctx)
	if err != nil || dev == nil {
		return false
	}
	return dev.ID != nil
}

func (c *Client) Connect() error {
	if c.WAClient == nil {
		return errors.New("client not initialized")
	}
	err := c.WAClient.Connect()
	if errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		return nil
	}
	return err
}

func (c *Client) Disconnect() {
	if c.WAClient != nil {
		c.WAClient.Disconnect()
	}
}

func (c *Client) GetQRChannel(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	if c.WAClient == nil {
		return nil, errors.New("client not initialized")
	}
	return c.WAClient.GetQRChannel(ctx)
}

// PairPhone requests an 8-character linking code. Must be called after Connect()
// and after the first QR event arrived (proves the socket is ready).
func (c *Client) PairPhone(ctx context.Context, phone string) (string, error) {
	if c.WAClient == nil {
		return "", errors.New("client not initialized")
	}
	digits := DigitsOnly(phone)
	if len(digits) < 7 {
		return "", errors.New("phone number too short")
	}
	if strings.HasPrefix(digits, "0") {
		return "", errors.New("use international format without a leading 0 (e.g. +92300...)")
	}
	return c.WAClient.PairPhone(ctx, digits, true, whatsmeow.PairClientChrome, PairDisplayName)
}

func (c *Client) SendTextMessage(ctx context.Context, to types.JID, text string) (whatsmeow.SendResponse, error) {
	if c.WAClient == nil {
		return whatsmeow.SendResponse{}, errors.New("client not initialized")
	}
	resp, err := c.WAClient.SendMessage(ctx, to, &waE2E.Message{Conversation: &text})
	if err == nil {
		c.markSent(resp.ID)
	}
	return resp, err
}

func (c *Client) WaitForConnection(d time.Duration) bool {
	return c.WAClient != nil && c.WAClient.WaitForConnection(d)
}

func (c *Client) IsConnected() bool {
	return c.WAClient != nil && c.WAClient.IsConnected()
}

func (c *Client) GetOwnJID() string {
	if c.WAClient == nil || c.WAClient.Store == nil || c.WAClient.Store.ID == nil {
		return ""
	}
	return "+" + c.WAClient.Store.ID.User
}

// ResetSession drops the stored device so a fresh login can start.
func (c *Client) ResetSession(ctx context.Context) error {
	if c.WAClient == nil || c.WAClient.Store == nil || c.WAClient.Store.ID == nil {
		return nil
	}
	c.WAClient.Disconnect()
	return c.WAClient.Store.Delete(ctx)
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.WAClient != nil {
			c.WAClient.Disconnect()
		}
		if c.store != nil {
			_ = c.store.Close()
		}
	})
}

// Event types consumed by the TUI.
type QRCodeEvent struct{ Code string }
type ConnectedEvent struct{}
type DisconnectedEvent struct{}
type LoggedOutEvent struct{ Reason string }
type PairSuccessEvent struct{ JID string }
type PairErrorEvent struct{ Err error }
type FatalEvent struct{ Err error }
type MessageReceivedEvent struct {
	Sender    string
	ReplyTo   types.JID
	Text      string
	MsgID     string
	Timestamp time.Time
}

// DigitsOnly strips everything except 0-9.
func DigitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
