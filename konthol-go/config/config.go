package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Credentials struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`
}

type WhatsAppConfig struct {
	Provider    string `json:"provider"`
	APIKey      string `json:"api_key"`
	TargetPhone string `json:"target_phone"`
	EndpointURL string `json:"endpoint_url"`
}

type TelegramConfig struct {
	Token  string `json:"token"`
	ChatID string `json:"chat_id"`
}

type AccountItem struct {
	Name           string `json:"name"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	WATarget       string `json:"wa_target"`
	TelegramChatID string `json:"telegram_chat_id"`
}

type AccountsConfig struct {
	WhatsApp WhatsAppConfig `json:"whatsapp"`
	Telegram TelegramConfig `json:"telegram"`
	Accounts []AccountItem  `json:"accounts"`
}

func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read credentials failed: %w", err)
	}

	var cred Credentials
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, fmt.Errorf("parse credentials failed: %w", err)
	}

	return &cred, nil
}

func LoadAccounts(path string) (*AccountsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var acc AccountsConfig
	if err := json.Unmarshal(data, &acc); err != nil {
		return nil, fmt.Errorf("parse accounts failed: %w", err)
	}

	return &acc, nil
}
