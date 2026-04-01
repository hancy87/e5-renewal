package notifier_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"e5-renewal/backend/services/notifier"
)

func TestNewService(t *testing.T) {
	svc := notifier.NewService()
	assert.NotNil(t, svc)
}

func TestSend_InvalidURL(t *testing.T) {
	svc := notifier.NewService()
	// shoutrrr.Send with an invalid URL should return an error
	err := svc.Send("not-a-valid-url", "Test Title", "Test Message")
	assert.Error(t, err)
}

func TestSend_EmptyURL(t *testing.T) {
	svc := notifier.NewService()
	err := svc.Send("", "Title", "Message")
	assert.Error(t, err)
}

func TestFormatTest(t *testing.T) {
	title, msg := notifier.FormatTest("ko")
	assert.Equal(t, "E5 Renewal", title)
	assert.Equal(t, "테스트 알림", msg)
	assert.NotContains(t, title, "✅")

	title, msg = notifier.FormatTest("en")
	assert.Equal(t, "E5 Renewal", title)
	assert.Equal(t, "Test notification", msg)
}

func TestFormatAuthExpiry(t *testing.T) {
	title, msg := notifier.FormatAuthExpiry("en", "MyAccount", 5)
	assert.NotContains(t, title, "⚠️")
	assert.Contains(t, title, "Client Secret")
	assert.Contains(t, msg, "MyAccount")
	assert.Contains(t, msg, "5")

	_, msg = notifier.FormatAuthExpiry("en", "MyAccount", -3)
	assert.Contains(t, msg, "3")

	title, msg = notifier.FormatAuthExpiry("ko", "MyAccount", 5)
	assert.Contains(t, title, "클라이언트 시크릿")
	assert.Contains(t, msg, "MyAccount")
}

func TestFormatTaskAllFailed(t *testing.T) {
	title, msg := notifier.FormatTaskAllFailed("en", "Acct1", 3)
	assert.NotContains(t, title, "❌")
	assert.Contains(t, msg, "Acct1")
	assert.Contains(t, msg, "3")

	_, msg = notifier.FormatTaskAllFailed("ko", "Acct1", 3)
	assert.Contains(t, msg, "Acct1")
}

func TestFormatHealthLow(t *testing.T) {
	title, msg := notifier.FormatHealthLow("en", "Acct2", 35.0, 50)
	assert.NotContains(t, title, "📉")
	assert.Contains(t, msg, "Acct2")
	assert.Contains(t, msg, "35")

	_, msg = notifier.FormatHealthLow("ko", "Acct2", 35.0, 50)
	assert.Contains(t, msg, "Acct2")
}
