package notify

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"miaomiaowu/internal/logger"
)

// Notifier manages sending Telegram notifications based on configuration.
type Notifier struct {
	mu  sync.RWMutex
	cfg Config
}

// New creates a Notifier with the given config.
func New(cfg Config) *Notifier {
	return &Notifier{cfg: cfg}
}

// UpdateConfig hot-reloads the notification configuration.
func (n *Notifier) UpdateConfig(cfg Config) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.cfg = cfg
}

// GetConfig returns a copy of the current configuration.
func (n *Notifier) GetConfig() Config {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.cfg
}

// IsEnabled checks whether the given event type should trigger a notification.
func (n *Notifier) IsEnabled(eventType EventType) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if !n.cfg.Enabled || n.cfg.BotToken == "" || n.cfg.ChatID == "" {
		return false
	}

	switch eventType {
	case EventSubscribeFetch:
		return n.cfg.NotifySubscribeFetch
	case EventLogin:
		return n.cfg.NotifyLogin
	case EventIPBan:
		return n.cfg.NotifyIPBan
	case EventSilentMode:
		return n.cfg.NotifySilentMode
	case EventDailyTraffic:
		return n.cfg.NotifyDailyTraffic
	case EventExpiry:
		return n.cfg.NotifyExpiry
	case EventNodeProbeOffline:
		return n.cfg.NotifyNodeProbeOffline
	case EventNodeProbeOnline:
		return n.cfg.NotifyNodeProbeOnline
	default:
		return false
	}
}

// Send dispatches a notification via Telegram if the event type is enabled.
func (n *Notifier) Send(ctx context.Context, event Event) error {
	if !n.IsEnabled(event.Type) {
		return nil
	}

	cfg := n.GetConfig()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	text := fmt.Sprintf("<b>%s</b>\n%s", EscapeHTML(event.Title), event.Message)
	err := sendTelegram(ctx, cfg.BotToken, cfg.ChatID, text)
	if err != nil {
		logger.Warn("[Notify] Telegram notification failed", "event_type", event.Type, "error", err)
	}
	return err
}

// SendTest sends a test message regardless of event type toggles.
func (n *Notifier) SendTest(ctx context.Context) error {
	cfg := n.GetConfig()
	if cfg.BotToken == "" || cfg.ChatID == "" {
		return fmt.Errorf("bot token or chat ID is empty")
	}
	return sendTelegram(ctx, cfg.BotToken, cfg.ChatID, "<b>测试通知</b>\n喵喵喵喵屋通知配置成功 ✓")
}

// SendDetached sends independently of the originating request's lifetime.
// Send supplies a deadline; callers may launch this method in a goroutine.
func (n *Notifier) SendDetached(event Event) error {
	return n.Send(context.Background(), event)
}

// EscapeHTML escapes dynamic text inserted into Telegram HTML messages.
func EscapeHTML(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}
