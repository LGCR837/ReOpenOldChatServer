package mail

import (
	"testing"
)

// TestSendCode_Real sends a real verification code email to 2067686978@qq.com
// using the SendCloud API. This test hits the real API and should be run
// manually with: go test -v -run TestSendCode_Real ./internal/mail/
func TestSendCode_Real(t *testing.T) {
	toEmail := "2067686978@qq.com"
	code := "836492"

	err := SendCode(toEmail, code)
	if err != nil {
		t.Fatalf("SendCode failed: %v", err)
	}
	t.Logf("邮件发送成功，收件人: %s, 验证码: %s", toEmail, code)
}

// TestDefaultSender checks that DefaultSender populates credentials correctly.
func TestDefaultSender(t *testing.T) {
	s := DefaultSender()
	if s.APIUser == "" {
		t.Error("APIUser should not be empty")
	}
	if s.APIKey == "" {
		t.Error("APIKey should not be empty")
	}
	if s.FromEmail == "" {
		t.Error("FromEmail should not be empty")
	}
	if s.FromName == "" {
		t.Error("FromName should not be empty")
	}
	if s.Client == nil {
		t.Error("Client should not be nil")
	}
	t.Logf("APIUser=%s, FromEmail=%s, FromName=%s", s.APIUser, s.FromEmail, s.FromName)
}
