package config

import (
	"encoding/json"
	"os"
)

type Credentials struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`
}

type AccountItem struct {
	Name           string `json:"name"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	WATarget       string `json:"wa_target"`
	TelegramChatID string `json:"telegram_chat_id"`
}

type MultiAccountConfig struct {
	WhatsApp struct {
		Provider    string `json:"provider"`
		APIKey      string `json:"api_key"`
		TargetPhone string `json:"target_phone"`
		EndpointURL string `json:"endpoint_url"`
	} `json:"whatsapp"`
	Telegram struct {
		Token  string `json:"token"`
		ChatID string `json:"chat_id"`
	} `json:"telegram"`
	Accounts []AccountItem `json:"accounts"`
}

func LoadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cred Credentials
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	return &cred, nil
}

func LoadMultiAccountConfig(path string) (*MultiAccountConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg MultiAccountConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
