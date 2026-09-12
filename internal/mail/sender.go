package mail

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const sendCloudAPIURL = "https://api2.sendcloud.net/api/mail/send"

// Sender holds SendCloud API credentials and sends emails via HTTP.
type Sender struct {
	APIUser   string
	APIKey    string
	FromEmail string
	FromName  string
	Client    *http.Client
}

// DefaultSender creates a Sender configured from environment variables,
// falling back to built-in defaults.
func DefaultSender() *Sender {
	apiUser := os.Getenv("SENDCLOUD_API_USER")
	if apiUser == "" {
		apiUser = os.Getenv("SENDLOUD_API_USER")
	}
	if apiUser == "" {
		apiUser = "oldchataacr"
	}

	apiKey := os.Getenv("SENDCLOUD_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("SENDLOUD_API_KEY")
	}
	if apiKey == "" {
		apiKey = "5f3c1bae18152f9a49c45a607e61c10e"
	}

	fromEmail := os.Getenv("SENDCLOUD_FROM_EMAIL")
	if fromEmail == "" {
		fromEmail = apiUser + "@mail.crweb.ccwu.cc"
	}

	fromName := os.Getenv("SENDCLOUD_FROM_NAME")
	if fromName == "" {
		fromName = "OldChat-AACR"
	}

	return &Sender{
		APIUser:   apiUser,
		APIKey:    apiKey,
		FromEmail: fromEmail,
		FromName:  fromName,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SendMail sends an email via the SendCloud API.
// toEmail can be a comma-separated list of recipients.
func (s *Sender) SendMail(toEmail, subject, content string) error {
	if s.APIUser == "" || s.APIKey == "" {
		return fmt.Errorf("mail credentials not configured; set SENDCLOUD_API_USER and SENDCLOUD_API_KEY")
	}

	form := url.Values{
		"apiUser":  {s.APIUser},
		"apiKey":   {s.APIKey},
		"from":     {s.FromEmail},
		"fromName": {s.FromName},
		"to":       {toEmail},
		"subject":  {subject},
		"plain":    {content},
	}

	resp, err := s.Client.PostForm(sendCloudAPIURL, form)
	if err != nil {
		return fmt.Errorf("mail request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response failed: %v", err)
	}

	var result struct {
		Result  bool   `json:"result"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("API response parse error: %s", strings.TrimSpace(string(body)))
	}

	if !result.Result {
		msg := result.Message
		if msg == "" {
			msg = "unknown error"
		}
		return fmt.Errorf("send failed: %s", msg)
	}

	return nil
}

// SendCode is the package-level convenience function used by the HTTP handlers.
// It sends a verification code email using the default sender.
func SendCode(toEmail, code string) error {
	sender := DefaultSender()
	subject := "旧聊验证码"
	content := fmt.Sprintf("你的验证码是: %s\n有效期10分钟。如非本人操作请忽略。", code)
	return sender.SendMail(toEmail, subject, content)
}
