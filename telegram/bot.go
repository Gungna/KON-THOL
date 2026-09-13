package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	Caption   string `json:"caption,omitempty"`
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
	Token             string
	AdminChatID       string
	HTTPClient        *http.Client
	LastMenuMsgID     int
	IsBannerActive    bool
	LastUpdateID      int
	EphemeralNotifIDs []int
	CommandHandler    func(cmd string, chatID int64, msgID int)
	ActionHandler     func(action string, chatID int64, msgID int, queryID string)
	mu                sync.Mutex
}

func NewTelegramBot(token, adminChatID string) *TelegramBot {
	return &TelegramBot{
		Token:             token,
		AdminChatID:       adminChatID,
		EphemeralNotifIDs: make([]int, 0),
		HTTPClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (b *TelegramBot) TrackEphemeral(msgID int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if msgID > 0 {
		b.EphemeralNotifIDs = append(b.EphemeralNotifIDs, msgID)
	}
}

func (b *TelegramBot) ClearEphemeralNotifications(chatID interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, id := range b.EphemeralNotifIDs {
		go b.DeleteMessage(chatID, id)
	}
	b.EphemeralNotifIDs = make([]int, 0)
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

func (b *TelegramBot) SendPhotoMenu(chatID interface{}, photoPath, caption string, keyboard *InlineKeyboardMarkup) (int, error) {
	file, err := os.Open(photoPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("photo", filepath.Base(photoPath))
	if err != nil {
		return 0, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return 0, err
	}

	_ = writer.WriteField("chat_id", fmt.Sprintf("%v", chatID))
	_ = writer.WriteField("caption", caption)
	_ = writer.WriteField("parse_mode", "HTML")

	if keyboard != nil {
		kbBytes, _ := json.Marshal(keyboard)
		_ = writer.WriteField("reply_markup", string(kbBytes))
	}
	writer.Close()

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendPhoto", b.Token)
	req, err := http.NewRequest(http.MethodPost, apiURL, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := b.HTTPClient.Do(req)
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
	return 0, fmt.Errorf("sendPhoto failed")
}

func (b *TelegramBot) EditMessageCaption(chatID interface{}, messageID int, caption string, keyboard *InlineKeyboardMarkup) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageCaption", b.Token)

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"caption":    caption,
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

func (b *TelegramBot) EditMessageText(chatID interface{}, messageID int, text string, keyboard *InlineKeyboardMarkup) error {
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

// ADAPTIVE LOADING BAR (Bervariasi sesuai kedalaman langkah, tidak kaku)
// Jika 1 langkah: langsung 100% seketika tanpa delay buatan
// Jika 2 langkah: 50% -> 100%
// Jika 3 langkah: 33% -> 66% -> 100%
func (b *TelegramBot) ShowAdaptiveProgress(chatID interface{}, step, totalSteps int, label string) int {
	if totalSteps <= 1 {
		msg := fmt.Sprintf("⏳ <b>%s</b>\n<code>[██████████] 100%%</code>", label)
		id, _ := b.SendMessage(chatID, msg, nil)
		return id
	}

	percent := int((float64(step) / float64(totalSteps)) * 100)
	filled := percent / 10
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)

	msg := fmt.Sprintf("⏳ <b>%s</b>\n<code>[%s] %d%%</code>", label, bar, percent)
	id, _ := b.SendMessage(chatID, msg, nil)
	return id
}

func (b *TelegramBot) UpdateAdaptiveProgress(chatID interface{}, msgID, step, totalSteps int, label string) {
	if msgID <= 0 || totalSteps <= 0 {
		return
	}
	percent := int((float64(step) / float64(totalSteps)) * 100)
	if percent > 100 {
		percent = 100
	}
	filled := percent / 10
	bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)

	msg := fmt.Sprintf("⏳ <b>%s</b>\n<code>[%s] %d%%</code>", label, bar, percent)
	_ = b.EditMessageText(chatID, msgID, msg, nil)
}

func (b *TelegramBot) FinishAdaptiveProgress(chatID interface{}, msgID int, label string) {
	if msgID <= 0 {
		return
	}
	msg := fmt.Sprintf("✅ <b>%s</b>\n<code>[██████████] 100%%</code>", label)
	_ = b.EditMessageText(chatID, msgID, msg, nil)
	time.Sleep(250 * time.Millisecond)
	b.DeleteMessage(chatID, msgID)
}

// In-place Single-View Menu with Banner (Zero Duplicate Menus)
func (b *TelegramBot) SendMenu(chatID interface{}, text string, keyboard *InlineKeyboardMarkup, bannerPaths []string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var activeBanner string
	for _, bp := range bannerPaths {
		if _, err := os.Stat(bp); err == nil {
			activeBanner = bp
			break
		}
	}

	if b.LastMenuMsgID > 0 {
		if b.IsBannerActive {
			err := b.EditMessageCaption(chatID, b.LastMenuMsgID, text, keyboard)
			if err == nil {
				return
			}
		}
		b.DeleteMessage(chatID, b.LastMenuMsgID)
		b.LastMenuMsgID = 0
		b.IsBannerActive = false
	}

	if activeBanner != "" {
		msgID, err := b.SendPhotoMenu(chatID, activeBanner, text, keyboard)
		if err == nil && msgID > 0 {
			b.LastMenuMsgID = msgID
			b.IsBannerActive = true
			return
		}
	}

	msgID, err := b.SendMessage(chatID, text, keyboard)
	if err == nil && msgID > 0 {
		b.LastMenuMsgID = msgID
		b.IsBannerActive = false
	}
}

// Show Content Card in-place (Zero Duplicate Message)
func (b *TelegramBot) ShowContentCard(chatID interface{}, text string, keyboard *InlineKeyboardMarkup) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.LastMenuMsgID > 0 {
		if b.IsBannerActive {
			b.DeleteMessage(chatID, b.LastMenuMsgID)
			b.LastMenuMsgID = 0
			b.IsBannerActive = false
		} else {
			err := b.EditMessageText(chatID, b.LastMenuMsgID, text, keyboard)
			if err == nil {
				return
			}
			b.DeleteMessage(chatID, b.LastMenuMsgID)
			b.LastMenuMsgID = 0
		}
	}

	msgID, err := b.SendMessage(chatID, text, keyboard)
	if err == nil && msgID > 0 {
		b.LastMenuMsgID = msgID
		b.IsBannerActive = false
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

					if b.ActionHandler != nil {
						go b.ActionHandler(action, chatID, msgID, u.CallbackQuery.ID)
					} else {
						b.AnswerCallbackQuery(u.CallbackQuery.ID, "")
					}
				}
			}

			time.Sleep(400 * time.Millisecond)
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

// 100% PARITAS KEYBOARDS

func GetMainKeyboard(page int, isCooldown bool) *InlineKeyboardMarkup {
	var cooldownBtn InlineKeyboardButton
	if isCooldown {
		cooldownBtn = InlineKeyboardButton{Text: "⚡ Batalkan Cooldown (Kembali Siaga)", CallbackData: "btn_resume"}
	} else {
		cooldownBtn = InlineKeyboardButton{Text: "💤 Istirahat / Cooldown Hari Ini", CallbackData: "btn_cooldown"}
	}

	if page == 2 {
		return &InlineKeyboardMarkup{
			InlineKeyboard: [][]InlineKeyboardButton{
				{
					{Text: "🔔 Notifikasi", CallbackData: "btn_notif"},
					{Text: "ℹ️ Status Sistem", CallbackData: "btn_status"},
				},
				{
					{Text: "🔄 Re-login Sesi", CallbackData: "btn_relogin"},
					{Text: "👥 Multi-Akun", CallbackData: "btn_konthol_public"},
				},
				{
					{Text: "❓ Bantuan", CallbackData: "btn_help"},
					{Text: "« Menu Utama", CallbackData: "btn_page_1"},
				},
			},
		}
	}

	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "⚡ Presensi Manual", CallbackData: "btn_scan"},
			},
			{
				{Text: "📅 Jadwal Kuliah", CallbackData: "btn_jadwal"},
				{Text: "📝 Tugas Pending", CallbackData: "btn_tugas"},
			},
			{
				{Text: "📊 Rekap Kehadiran", CallbackData: "btn_rekap"},
				{Text: "📜 Log Aktivitas", CallbackData: "btn_log"},
			},
			{
				cooldownBtn,
			},
			{
				{Text: "⏩ Menu Lanjutan (Hal 2) »", CallbackData: "btn_page_2"},
			},
		},
	}
}

func GetStatusKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "« Menu Utama", CallbackData: "btn_page_1"},
				{Text: "« Balik ke Menu 2", CallbackData: "btn_page_2"},
			},
		},
	}
}

func GetPage2BackKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "« Menu Utama", CallbackData: "btn_page_1"},
				{Text: "« Balik ke Menu 2", CallbackData: "btn_page_2"},
			},
		},
	}
}

func GetKontholPublicKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "⚡ Scan Semua Akun Sekarang", CallbackData: "btn_public_scan"},
			},
			{
				{Text: "📋 Daftar Akun Terdaftar", CallbackData: "btn_public_accounts"},
			},
			{
				{Text: "« Menu Utama", CallbackData: "btn_page_1"},
				{Text: "« Balik ke Menu 2", CallbackData: "btn_page_2"},
			},
		},
	}
}

func GetLogKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "⚠️ Log Error", CallbackData: "btn_log_err"},
				{Text: "🔑 Log Login & Listener", CallbackData: "btn_log_auth"},
			},
			{
				{Text: "🔔 Log Notif Masuk", CallbackData: "btn_log_notif"},
				{Text: "✅ Log Presensi Berhasil", CallbackData: "btn_log_pres"},
			},
			{
				{Text: "🌐 Log Semua (Global)", CallbackData: "btn_log_all"},
			},
			{
				{Text: "« Kembali ke Menu Utama", CallbackData: "btn_menu"},
			},
		},
	}
}

func GetBackKeyboard() *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "« Kembali ke Menu Utama", CallbackData: "btn_menu"},
			},
		},
	}
}
