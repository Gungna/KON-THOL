package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	Date      int64  `json:"date"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data"`
}

type TelegramBot struct {
	Token          string
	AdminChatID    string
	HTTPClient     *http.Client
	LastMenuMsgID  int
	LastUpdateID   int
	CommandHandler func(cmd string, chatID int64, msgID int)
	ActionHandler  func(action string, chatID int64, msgID int, queryID string)
	mu             sync.Mutex
}

func NewTelegramBot(token, adminChatID string) *TelegramBot {
	return &TelegramBot{
		Token:       token,
		AdminChatID: adminChatID,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (b *TelegramBot) SendMessage(chatID interface{}, text string, keyboard *InlineKeyboardMarkup) (int, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.Token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	pBytes, _ := json.Marshal(payload)
	resp, err := b.HTTPClient.Post(apiURL, "application/json", bytes.NewReader(pBytes))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var res struct {
		OK     bool    `json:"ok"`
		Result Message `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, err
	}
	if res.OK {
		return res.Result.MessageID, nil
	}
	return 0, fmt.Errorf("telegram API error on sendMessage")
}

func (b *TelegramBot) EditMessage(chatID interface{}, messageID int, text string, keyboard *InlineKeyboardMarkup) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", b.Token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	pBytes, _ := json.Marshal(payload)
	resp, err := b.HTTPClient.Post(apiURL, "application/json", bytes.NewReader(pBytes))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (b *TelegramBot) DeleteMessage(chatID interface{}, messageID int) {
	if messageID <= 0 {
		return
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/deleteMessage", b.Token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	pBytes, _ := json.Marshal(payload)
	resp, err := b.HTTPClient.Post(apiURL, "application/json", bytes.NewReader(pBytes))
	if err == nil {
		resp.Body.Close()
	}
}

func (b *TelegramBot) AnswerCallbackQuery(queryID string, text string) {
	if queryID == "" {
		return
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", b.Token)
	payload := map[string]interface{}{
		"callback_query_id": queryID,
	}
	if text != "" {
		payload["text"] = text
	}
	pBytes, _ := json.Marshal(payload)
	resp, err := b.HTTPClient.Post(apiURL, "application/json", bytes.NewReader(pBytes))
	if err == nil {
		resp.Body.Close()
	}
}

// Single-View Menu: replace in-place or send new and record message ID
func (b *TelegramBot) ShowMenu(chatID interface{}, text string, keyboard *InlineKeyboardMarkup) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.LastMenuMsgID > 0 {
		err := b.EditMessage(chatID, b.LastMenuMsgID, text, keyboard)
		if err == nil {
			return
		}
		// If edit failed (e.g. message too old or deleted), send fresh
		b.DeleteMessage(chatID, b.LastMenuMsgID)
	}

	newID, err := b.SendMessage(chatID, text, keyboard)
	if err == nil && newID > 0 {
		b.LastMenuMsgID = newID
	}
}

func (b *TelegramBot) StartPolling(stopChan <-chan struct{}) {
	log.Println("[TELEGRAM] Memulai polling Telegram bot...")

	for {
		select {
		case <-stopChan:
			log.Println("[TELEGRAM] Polling dihentikan.")
			return
		default:
			updates, err := b.getUpdates(b.LastUpdateID + 1)
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}

			for _, u := range updates {
				if u.UpdateID > b.LastUpdateID {
					b.LastUpdateID = u.UpdateID
				}

				if u.Message != nil && u.Message.Text != "" {
					cmd := u.Message.Text
					chatID := u.Message.Chat.ID
					msgID := u.Message.MessageID

					// Delete slash command message from user to keep chat clean
					go b.DeleteMessage(chatID, msgID)

					if b.CommandHandler != nil {
						go b.CommandHandler(cmd, chatID, msgID)
					}
				}

				if u.CallbackQuery != nil {
					action := u.CallbackQuery.Data
					var chatID int64
					var msgID int
					if u.CallbackQuery.Message != nil {
						chatID = u.CallbackQuery.Message.Chat.ID
						msgID = u.CallbackQuery.Message.MessageID
					}
					b.AnswerCallbackQuery(u.CallbackQuery.ID, "")

					if b.ActionHandler != nil {
						go b.ActionHandler(action, chatID, msgID, u.CallbackQuery.ID)
					}
				}
			}

			time.Sleep(500 * time.Millisecond)
		}
	}
}

func (b *TelegramBot) getUpdates(offset int) ([]Update, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=15", b.Token, offset)
	resp, err := b.HTTPClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bad status %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Result, nil
}

// UI Keyboards
func GetMainKeyboard(page int) *InlineKeyboardMarkup {
	if page == 2 {
		return &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{Text: "📚 Kuliah Saya", CallbackData: "btn_courses"},
					{Text: "📝 Tugas Kuliah", CallbackData: "btn_tugas"},
				},
				{
					{Text: "⏸️ Mode Cooldown", CallbackData: "btn_cooldown"},
					{Text: "🟢 Siaga Penuh", CallbackData: "btn_resume"},
				},
				{
					{Text: "👥 Multi-Akun", CallbackData: "btn_accounts"},
					{Text: "🔄 Relogin CAS", CallbackData: "btn_relogin"},
				},
				{
					{Text: "⏮️ Halaman 1", CallbackData: "btn_page_1"},
				},
			},
		}
	}

	// Page 1 Default
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "📊 Status Bot", CallbackData: "btn_status"},
				{Text: "🔄 Scan Presensi", CallbackData: "btn_scan"},
			},
			{
				{Text: "📅 Jadwal Hari Ini", CallbackData: "btn_jadwal_hari_ini"},
				{Text: "📋 Jadwal Lengkap", CallbackData: "btn_jadwal_lengkap"},
			},
			{
				{Text: "📈 Rekapitulasi", CallbackData: "btn_rekap"},
				{Text: "🔔 Cek Notifikasi", CallbackData: "btn_notif"},
			},
			{
				{Text: "⏭️ Halaman 2", CallbackData: "btn_page_2"},
			},
		},
	}
}

func GetBackKeyboard(page int) *InlineKeyboardMarkup {
	target := "btn_page_1"
	if page == 2 {
		target = "btn_page_2"
	}
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "⬅️ Kembali ke Menu", CallbackData: target},
			},
		},
	}
}
