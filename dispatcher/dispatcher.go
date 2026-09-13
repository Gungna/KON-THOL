package dispatcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type WhatsAppConfig struct {
	Provider    string `json:"provider"`     // fonnte, waha, evo/evolution, wppconnect, webhook
	APIKey      string `json:"api_key"`      // API Token / Secret
	TargetPhone string `json:"target_phone"` // Default target phone
	EndpointURL string `json:"endpoint_url"` // Custom server endpoint
	SessionName string `json:"session_name"` // Session name for WAHA / Evolution instance
}

type NotificationDispatcher struct {
	WAConfig   WhatsAppConfig
	HTTPClient *http.Client
}

func NewNotificationDispatcher(waConfig WhatsAppConfig) *NotificationDispatcher {
	return &NotificationDispatcher{
		WAConfig: waConfig,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendWhatsApp sends alert via Fonnte, WAHA, Evolution API (Evo), WPPConnect, or Generic Webhook
func (d *NotificationDispatcher) SendWhatsApp(phone, text string) error {
	provider := strings.ToLower(strings.TrimSpace(d.WAConfig.Provider))
	target := phone
	if target == "" {
		target = d.WAConfig.TargetPhone
	}
	if target == "" || provider == "" || provider == "none" {
		return nil
	}

	endpoint := d.WAConfig.EndpointURL

	switch provider {
	case "fonnte":
		if endpoint == "" {
			endpoint = "https://api.fonnte.com/send"
		}
		payload := map[string]string{
			"target":  target,
			"message": text,
		}
		pBytes, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(pBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if d.WAConfig.APIKey != "" {
			req.Header.Set("Authorization", d.WAConfig.APIKey)
		}

		resp, err := d.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[WA] Alert berhasil terkirim via Fonnte ke %s", target)
			return nil
		}
		return fmt.Errorf("fonnte status: %d", resp.StatusCode)

	case "waha":
		// WAHA (WhatsApp HTTP API) - Core & Plus Edition
		if endpoint == "" {
			endpoint = "http://localhost:3000/api/sendText"
		}
		chatID := target
		if !strings.Contains(chatID, "@") {
			chatID = chatID + "@c.us"
		}
		session := d.WAConfig.SessionName
		if session == "" {
			session = "default"
		}

		payload := map[string]interface{}{
			"chatId":  chatID,
			"text":    text,
			"session": session,
		}
		pBytes, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(pBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if d.WAConfig.APIKey != "" {
			req.Header.Set("X-Api-Key", d.WAConfig.APIKey)
			req.Header.Set("Authorization", "Bearer "+d.WAConfig.APIKey)
		}

		resp, err := d.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[WA] Alert berhasil terkirim via WAHA ke %s", target)
			return nil
		}
		return fmt.Errorf("waha status: %d", resp.StatusCode)

	case "evo", "evolution", "evolution-api":
		// Evolution API v1 / v2
		instance := d.WAConfig.SessionName
		if instance == "" {
			instance = "default"
		}
		if endpoint == "" {
			endpoint = fmt.Sprintf("http://localhost:8080/message/sendText/%s", instance)
		} else if !strings.Contains(endpoint, "/message/sendText") {
			endpoint = strings.TrimRight(endpoint, "/") + "/message/sendText/" + instance
		}

		cleanNumber := strings.ReplaceAll(strings.ReplaceAll(target, "+", ""), "-", "")
		payload := map[string]interface{}{
			"number": cleanNumber,
			"text":   text,
		}
		pBytes, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(pBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if d.WAConfig.APIKey != "" {
			req.Header.Set("apikey", d.WAConfig.APIKey)
		}

		resp, err := d.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[WA] Alert berhasil terkirim via Evolution API ke %s", target)
			return nil
		}
		return fmt.Errorf("evolution api status: %d", resp.StatusCode)

	case "webhook", "generic", "wppconnect":
		if endpoint == "" {
			return fmt.Errorf("endpoint_url kosong untuk provider webhook/wppconnect")
		}
		payload := map[string]string{
			"phone":   target,
			"message": text,
		}
		pBytes, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(pBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if d.WAConfig.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+d.WAConfig.APIKey)
		}

		resp, err := d.HTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[WA] Alert berhasil terkirim via Webhook ke %s", target)
			return nil
		}
		return fmt.Errorf("webhook status: %d", resp.StatusCode)
	}

	return nil
}

// SendLocalOSNotification handles Termux (Android), Linux notify-send, and Windows Toast
func SendLocalOSNotification(title, message string) {
	plainText := strings.ReplaceAll(message, "<b>", "")
	plainText = strings.ReplaceAll(plainText, "</b>", "")
	plainText = strings.ReplaceAll(plainText, "<i>", "")
	plainText = strings.ReplaceAll(plainText, "</i>", "")
	plainText = strings.ReplaceAll(plainText, "<code>", "")
	plainText = strings.ReplaceAll(plainText, "</code>", "")
	if len(plainText) > 120 {
		plainText = plainText[:117] + "..."
	}

	switch runtime.GOOS {
	case "android", "linux":
		if path, err := exec.LookPath("termux-notification"); err == nil && path != "" {
			cmd := exec.Command("termux-notification", "--title", title, "--content", plainText, "--priority", "high")
			_ = cmd.Run()
			return
		}
		if path, err := exec.LookPath("notify-send"); err == nil && path != "" {
			cmd := exec.Command("notify-send", title, plainText)
			_ = cmd.Run()
			return
		}

	case "windows":
		psScript := fmt.Sprintf(
			`[reflection.assembly]::loadwithpartialname('System.Windows.Forms') | Out-Null; `+
				`$notify = New-Object System.Windows.Forms.NotifyIcon; `+
				`$notify.Icon = [System.Drawing.SystemIcons]::Information; `+
				`$notify.Visible = $true; `+
				`$notify.ShowBalloonTip(0, '%s', '%s', [System.Windows.Forms.ToolTipIcon]::Info); `+
				`Start-Sleep -Seconds 1; $notify.Dispose()`,
			escapePS(title), escapePS(plainText),
		)
		cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
		_ = cmd.Run()
	}
}

func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
