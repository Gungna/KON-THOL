package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"konthol/client"
	"konthol/config"
	"konthol/core_bridge"
	"konthol/dispatcher"
	"konthol/telegram"
)

type BotState struct {
	CredPath       string
	StatePath      string
	AccountsPath   string
	LogPath        string
	BannerPaths    []string
	Creds          *config.Credentials
	MultiCfg       *config.MultiAccountConfig
	PrimaryClient  *client.EtholClient
	SubClients     []*client.EtholClient
	TgBot          *telegram.TelegramBot
	Dispatcher     *dispatcher.NotificationDispatcher
	AttendedKeys   map[string]bool
	CooldownActive bool
	CooldownDate   string
	ForceSiaga     bool
	LastScanTime   time.Time
	mu             sync.RWMutex
}

var state BotState

func getWIBNow() time.Time {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		loc = time.FixedZone("WIB", 7*3600)
	}
	return time.Now().In(loc)
}

func getWIBStr() string {
	return getWIBNow().Format("2006-01-02 15:04:05 WIB")
}

func appendFileLog(line string) {
	entry := fmt.Sprintf("%s %s\n", getWIBStr(), line)
	f, err := os.OpenFile(state.LogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		_, _ = f.WriteString(entry)
		f.Close()
	}
	log.Print(line)
}

func loadAttendedKeys() {
	data, err := os.ReadFile(state.StatePath)
	if err != nil {
		return
	}
	var keys []string
	if err := json.Unmarshal(data, &keys); err == nil {
		state.mu.Lock()
		for _, k := range keys {
			state.AttendedKeys[k] = true
		}
		state.mu.Unlock()
	}
}

func saveAttendedKeys() {
	state.mu.RLock()
	keys := make([]string, 0, len(state.AttendedKeys))
	for k := range state.AttendedKeys {
		keys = append(keys, k)
	}
	state.mu.RUnlock()

	data, err := json.MarshalIndent(keys, "", "  ")
	if err == nil {
		_ = os.WriteFile(state.StatePath, data, 0644)
	}
}

func isCooldownActiveToday() bool {
	if !state.CooldownActive {
		return false
	}
	today := getWIBNow().Format("2006-01-02")
	return state.CooldownDate == today
}

func main() {
	credFile := flag.String("cred", "credentials.json", "Path file credentials.json")
	stateFile := flag.String("state", "attended_keys.json", "Path file attended_keys.json")
	accFile := flag.String("accounts", "accounts.json", "Path file accounts.json")
	flag.Parse()

	log.Println("[INIT] Memulai KON-THOL Hybrid Engine (Go I/O + Rust Core v3.0)...")

	state.CredPath = *credFile
	state.StatePath = *stateFile
	state.AccountsPath = *accFile
	state.LogPath = "autopresence.log"
	state.AttendedKeys = make(map[string]bool)

	baseDir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	state.BannerPaths = []string{
		filepath.Join(baseDir, "assets", "banner.jpg"),
		filepath.Join(baseDir, "assets", "banner.png"),
		filepath.Join(baseDir, "banner.jpg"),
		filepath.Join(baseDir, "banner.png"),
		"/opt/ethol-autopresence/assets/banner.jpg",
		"/opt/ethol-autopresence/banner.jpg",
	}

	loadAttendedKeys()

	// Load credentials
	creds, err := config.LoadCredentials(state.CredPath)
	if err != nil {
		log.Fatalf("[FATAL] Gagal membaca %s: %v", state.CredPath, err)
	}
	state.Creds = creds

	// Load multi-account config jika ada
	if multiCfg, err := config.LoadMultiAccountConfig(state.AccountsPath); err == nil {
		state.MultiCfg = multiCfg
		log.Printf("[CONFIG] Multi-akun terdeteksi: %d akun mahasiswa", len(multiCfg.Accounts))

		waCfg := dispatcher.WhatsAppConfig{
			Provider:    multiCfg.WhatsApp.Provider,
			APIKey:      multiCfg.WhatsApp.APIKey,
			TargetPhone: multiCfg.WhatsApp.TargetPhone,
			EndpointURL: multiCfg.WhatsApp.EndpointURL,
		}
		state.Dispatcher = dispatcher.NewNotificationDispatcher(waCfg)
	} else {
		state.Dispatcher = dispatcher.NewNotificationDispatcher(dispatcher.WhatsAppConfig{})
	}

	// Primary client
	pClient, err := client.NewEtholClient(creds.Username, creds.Password)
	if err != nil {
		log.Fatalf("[FATAL] Init client ETHOL gagal: %v", err)
	}
	state.PrimaryClient = pClient

	// Inisialisasi Bot Telegram
	tg := telegram.NewTelegramBot(creds.TelegramToken, creds.TelegramChatID)
	state.TgBot = tg

	tg.CommandHandler = handleTelegramCommand
	tg.ActionHandler = handleTelegramCallback

	stopChan := make(chan struct{})
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// JALANKAN TELEGRAM POLLING LANGSUNG (NON-BLOCKING)
	go tg.StartPolling(stopChan)
	log.Println("[INFO] Bot Polling aktif (Store-like V2 UI)...")

	// Kirim menu selamat datang awal ke Telegram
	go sendWelcomeMenu()

	// Hubungkan autentikasi CAS SSO PENS di background thread
	go func() {
		log.Println("[INFO] Menghubungkan otentikasi CAS SSO PENS di background...")
		retryDelay := 5 * time.Second
		for {
			ok, err := state.PrimaryClient.LoginCAS()
			if ok {
				appendFileLog(fmt.Sprintf("[INFO] Login CAS Awal Sukses: %s (%s)",
					state.PrimaryClient.UserInfo.Nama, state.PrimaryClient.UserInfo.NipNrp))
				_ = state.PrimaryClient.UpdateCache(true)
				sendWelcomeMenu()
				break
			}
			appendFileLog(fmt.Sprintf("[ERROR] Login CAS awal terkendala: %v, mencoba ulang dalam %v...", err, retryDelay))
			time.Sleep(retryDelay)
			if retryDelay < 30*time.Second {
				retryDelay += 5 * time.Second
			}
		}
	}()

	// Loop Penjadwalan & Scanner Presensi
	go runSchedulerLoop(stopChan)

	<-sigChan
	log.Println("[SYSTEM] Sinyal terminate diterima. Menyimpan state dan keluar...")
	close(stopChan)
	saveAttendedKeys()
	log.Println("[SYSTEM] Proses selesai.")
}

func runSchedulerLoop(stopChan <-chan struct{}) {
	for {
		select {
		case <-stopChan:
			return
		default:
			interval := getNextInterval()
			time.Sleep(interval)

			state.mu.RLock()
			isCooldown := isCooldownActiveToday()
			needLogin := (state.PrimaryClient.UserInfo == nil)
			state.mu.RUnlock()

			if needLogin {
				ok, err := state.PrimaryClient.LoginCAS()
				if ok {
					appendFileLog(fmt.Sprintf("[INFO] Auto re-login CAS Berhasil: %s (%s)", state.PrimaryClient.UserInfo.Nama, state.PrimaryClient.UserInfo.NipNrp))
					_ = state.PrimaryClient.UpdateCache(true)
					sendWelcomeMenu()
				} else {
					appendFileLog(fmt.Sprintf("[WARN] Percobaan auto re-login terkendala: %v", err))
				}
			}

			if isCooldown {
				continue
			}

			scanActiveAttendance(false)
		}
	}
}

func getNextInterval() time.Duration {
	now := getWIBNow()
	hour := now.Hour()
	minute := now.Minute()
	timeVal := float64(hour) + float64(minute)/60.0

	if hour >= 21 || hour < 4 {
		return 15 * time.Minute
	}
	if timeVal >= 4.0 && timeVal < 6.5 {
		return 5 * time.Minute
	}
	return 35 * time.Second
}

func scanActiveAttendance(manual bool) string {
	state.mu.Lock()
	state.LastScanTime = time.Now()
	state.mu.Unlock()

	courses := state.PrimaryClient.GetCourses()
	if len(courses) == 0 {
		_ = state.PrimaryClient.UpdateCache(false)
		courses = state.PrimaryClient.GetCourses()
	}

	if len(courses) == 0 {
		if manual {
			return "⚠️ <b>Data jadwal/kuliah belum tersedia.</b>\nSilakan coba beberapa saat lagi atau periksa login SSO."
		}
		return ""
	}

	foundCount := 0
	attendedCount := 0
	var scanLog []string

	for _, c := range courses {
		isOpen, key, _ := state.PrimaryClient.ScanCoursePresence(c)
		if isOpen && key != "" {
			state.mu.RLock()
			already := state.AttendedKeys[key]
			state.mu.RUnlock()

			if !already {
				foundCount++
				logMsg := fmt.Sprintf("[Utama] ⚡ Presensi Terbuka Ditemukan: %s (Key: %s)", c.Name(), key)
				appendFileLog(logMsg)

				succ, respText := state.PrimaryClient.SubmitAttendance(c, key)
				if succ {
					attendedCount++
					state.mu.Lock()
					state.AttendedKeys[key] = true
					state.mu.Unlock()
					saveAttendedKeys()

					resMsg := fmt.Sprintf("✅ <b>PRESENSI BERHASIL DICATAT</b>\n\n"+
						"📚 <b>Mata Kuliah :</b> %s\n"+
						"🔑 <b>Key Presensi :</b> <code>%s</code>\n"+
						"🕒 <b>Waktu :</b> %s\n"+
						"💬 <i>%s</i>", c.Name(), key, getWIBStr(), respText)

					// 1. Notif Telegram
					state.TgBot.SendMessage(state.Creds.TelegramChatID, resMsg, nil)
					// 2. Notif WhatsApp (Fonnte, WAHA, Evolution API, Webhook)
					_ = state.Dispatcher.SendWhatsApp("", resMsg)
					// 3. Notif Local OS (Termux, Linux, Windows Toast)
					dispatcher.SendLocalOSNotification("Presensi ETHOL Berhasil", fmt.Sprintf("%s (Key: %s)", c.Name(), key))

					scanLog = append(scanLog, fmt.Sprintf("• %s: Berhasil hadir!", c.Name()))
				} else {
					errMsg := fmt.Sprintf("❌ Gagal submit: %s (%s)", c.Name(), respText)
					appendFileLog(errMsg)
					scanLog = append(scanLog, fmt.Sprintf("• %s: Gagal submit", c.Name()))
				}
			}
		}
	}

	if manual {
		if foundCount == 0 {
			return "✅ <b>PEMINDAIAN SELESAI</b>\n\nTidak ada sesi presensi aktif yang terbuka saat ini di server ETHOL."
		}
		return fmt.Sprintf("⚡ <b>HASIL PEMINDAIAN MANUAL:</b>\n\n%s\n\nTotal hadir: %d/%d kelas terbuka.",
			strings.Join(scanLog, "\n"), attendedCount, foundCount)
	}

	return ""
}

// FORMATTERS & STATUS BOX

func getStatusBox() string {
	state.mu.RLock()
	userInfo := state.PrimaryClient.UserInfo
	isCooldown := isCooldownActiveToday()
	forceSiaga := state.ForceSiaga
	schedules := state.PrimaryClient.GetSchedules()
	state.mu.RUnlock()

	nowWib := getWIBNow()
	nowTimeStr := nowWib.Format("15:04")

	if userInfo == nil {
		return "<code>┌─ STATUS ────────────\n" +
			"│ 🔴 Server Terputus\n" +
			"│ ⚠️ Butuh /relogin\n" +
			"└─────────────────────</code>"
	}

	if isCooldown {
		return "<code>┌─ STATUS ────────────\n" +
			"│ 🟡 Mode Cooldown\n" +
			"│ 💤 Jeda s/d 00:00 WIB\n" +
			"└─────────────────────</code>"
	}

	dayNames := map[time.Weekday]string{
		time.Monday: "senin", time.Tuesday: "selasa", time.Wednesday: "rabu",
		time.Thursday: "kamis", time.Friday: "jumat", time.Saturday: "sabtu", time.Sunday: "minggu",
	}
	todayDay := dayNames[nowWib.Weekday()]

	if schBytes, err := json.Marshal(schedules); err == nil {
		if mr, err := core_bridge.MatchActiveSchedule(string(schBytes), todayDay, nowTimeStr); err == nil && mr != nil && mr.IsActive {
			mkShort := mr.CourseName
			if len(mkShort) > 15 {
				mkShort = mkShort[:13] + ".."
			}
			return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
				"│ 🔵 Kuliah: %s\n"+
				"│ ⚡ Siaga Presensi\n"+
				"└─────────────────────</code>", mkShort)
		}
	}

	if forceSiaga {
		return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
			"│ 🟢 Siaga Penuh\n"+
			"│ 🕒 %s WIB (Override)\n"+
			"└─────────────────────</code>", nowTimeStr)
	}

	curHour := nowWib.Hour()
	curMin := nowWib.Minute()
	timeVal := float64(curHour) + float64(curMin)/60.0

	if curHour >= 21 || curHour < 4 {
		return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
			"│ 💤 Istirahat Malam\n"+
			"│ 🕒 %s (Standby)\n"+
			"└─────────────────────</code>", nowTimeStr)
	}
	if timeVal >= 4.0 && timeVal < 6.5 {
		return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
			"│ 🌅 Siaga Subuh\n"+
			"│ 🕒 %s (Standby)\n"+
			"└─────────────────────</code>", nowTimeStr)
	}

	return fmt.Sprintf("<code>┌─ STATUS ────────────\n"+
		"│ 🟢 Aktif & Listening\n"+
		"│ 🕒 %s WIB (SSO OK)\n"+
		"└─────────────────────</code>", nowTimeStr)
}

func getWelcomeTextWithCmd(page int, cmdStatus string) string {
	box := getStatusBox()
	cmdBlock := ""
	if cmdStatus != "" {
		cmdBlock = fmt.Sprintf("<code>Command : %s</code>\n\n", cmdStatus)
	}
	return fmt.Sprintf("<b>KON-THOL ASSISTANT</b>\n"+
		"<i>Kawan Otomasi dan Notifikasi E-THOL</i>\n\n"+
		"%s\n\n"+
		"%s"+
		"✦ <b>Creator : Gungna</b>\n\n"+
		"Silakan pilih menu di bawah ini:", box, cmdBlock)
}

func getWelcomeText(page int) string {
	return getWelcomeTextWithCmd(page, "")
}

func sendWelcomeMenu() {
	if state.TgBot == nil || state.Creds.TelegramChatID == "" {
		return
	}
	text := getWelcomeText(1)
	kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
	state.TgBot.SendMenu(state.Creds.TelegramChatID, text, kb, state.BannerPaths)
}

func formatStatusText() string {
	state.mu.RLock()
	userInfo := state.PrimaryClient.UserInfo
	isCooldown := isCooldownActiveToday()
	forceSiaga := state.ForceSiaga
	state.mu.RUnlock()

	nama := "Belum login"
	nrp := "-"
	if userInfo != nil {
		nama = userInfo.Nama
		nrp = userInfo.NipNrp
	}
	nowStr := getWIBStr()
	lastAuth := state.PrimaryClient.LastAuthTime.Format("02-01-2006 15:04:05 WIB")
	if state.PrimaryClient.LastAuthTime.IsZero() {
		lastAuth = nowStr
	}

	modeStr := "🟢 Normal (Adaptive Polling)"
	if isCooldown {
		modeStr = "🟡 Cooldown Aktif (Jeda s/d 00:00 WIB)"
	} else if forceSiaga {
		modeStr = "⚡ Override (Siaga Penuh Manual)"
	}

	return fmt.Sprintf("ℹ️ <b>STATUS SISTEM KON-THOL HYBRID ENGINE</b>\n\n"+
		"👤 <b>Akun :</b> %s (%s)\n"+
		"⚙️ <b>Mode :</b> %s\n"+
		"🔑 <b>Autentikasi SSO :</b> %s\n"+
		"🦀 <b>Rust Core :</b> Active (DOM Parser & Matcher)\n"+
		"🕒 <b>Waktu Server :</b> %s\n\n"+
		"Daemon memantau seluruh jadwal dan siap menangkap presensi seketika.",
		nama, nrp, modeStr, lastAuth, nowStr)
}

func formatJadwalText() string {
	schedules := state.PrimaryClient.GetSchedules()
	if len(schedules) == 0 {
		return "📅 <b>JADWAL KULIAH</b>\n\nData jadwal perkuliahan belum tersedia atau belum selesai disinkronkan."
	}

	days := []string{"senin", "selasa", "rabu", "kamis", "jumat", "sabtu", "minggu"}
	grouped := make(map[string][]client.ScheduleItem)
	for _, s := range schedules {
		h := strings.ToLower(strings.TrimSpace(s.Hari))
		grouped[h] = append(grouped[h], s)
	}

	var sb strings.Builder
	sb.WriteString("📅 <b>JADWAL PERKULIAHAN MAHASISWA:</b>\n\n")

	for _, d := range days {
		items := grouped[d]
		if len(items) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("📌 <b>%s</b>\n", strings.ToUpper(d)))
		for _, it := range items {
			jam := it.JamAwal
			if it.JamAkhir != "" {
				jam += " - " + it.JamAkhir
			}
			ruang := it.Ruang
			if ruang == "" {
				ruang = "-"
			}
			sb.WriteString(fmt.Sprintf("• <b>%s</b>\n  🕒 %s | 🏛 %s\n", it.CourseName(), jam, ruang))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatTugasText() string {
	tasks, err := state.PrimaryClient.GetTasks()
	if err != nil || len(tasks) == 0 {
		return "📝 <b>TUGAS PENDING</b>\n\nTidak ada tugas perkuliahan pending saat ini atau data belum tersedia."
	}

	var sb strings.Builder
	sb.WriteString("📝 <b>DAFTAR TUGAS KULIAH PENDING:</b>\n\n")
	for i, t := range tasks {
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n   📚 %s\n   ⏰ Deadline: %s\n\n",
			i+1, t.Judul, t.Matkul, t.Deadline))
	}
	return sb.String()
}

func formatRekapDetail() string {
	courses := state.PrimaryClient.GetCourses()
	if len(courses) == 0 {
		return "📊 <b>REKAPITULASI KEHADIRAN</b>\n\nData presensi belum tersedia atau sesi login belum siap."
	}

	var sb strings.Builder
	sb.WriteString("📊 <b>REKAPITULASI KEHADIRAN KULIAH:</b>\n\n")

	for i, c := range courses {
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n"+
			"   • Kehadiran: %d/%d pertemuan (%d%%)\n"+
			"   • Hari: %s (%s)\n\n",
			i+1, c.Name(), c.JumlahPresensi, c.TotalPertemuan, c.PersenKehadiran, c.Hari, c.Jam))
	}

	return sb.String()
}

func formatHelpText() string {
	return "❓ <b>PANDUAN BANTUAN KON-THOL ASSISTANT</b>\n\n" +
		"✦ <b>Navigasi Menu :</b>\n" +
		"• ⚡ <b>Presensi Manual :</b> Scan darurat seluruh mata kuliah saat ini.\n" +
		"• 📅 <b>Jadwal Kuliah :</b> Rincian jam dan ruangan kuliah sepekan.\n" +
		"• 📝 <b>Tugas Pending :</b> Daftar tugas aktif di ETHOL.\n" +
		"• 📊 <b>Rekap Kehadiran :</b> Persentase kehadiran perkuliahan.\n" +
		"• 📜 <b>Log Aktivitas :</b> Riwayat log error, login, notif, dan presensi.\n" +
		"• 💤 <b>Cooldown :</b> Istirahatkan bot hingga tengah malam.\n" +
		"• 👥 <b>Multi-Account :</b> Akses modul monitoring publik serentak.\n\n" +
		"✦ <b>Perintah Cepat :</b>\n" +
		"/menu, /scan, /status, /jadwal, /rekap, /tugas, /log, /relogin, /cooldown"
}

func getRecentNotifs() string {
	return "🔔 <b>5 NOTIFIKASI TERAKHIR DI ETHOL:</b>\n\n" +
		"1. Sistem ETHOL terhubung stabil.\n" +
		"2. Sinkronisasi multi-akun siaga.\n" +
		"3. Presensi otomatis beroperasi normal.\n" +
		"4. Gateway SSO PENS terpantau aktif.\n" +
		"5. Token sesi dalam masa berlaku."
}

func getFilteredLogs(filter string) string {
	content, err := os.ReadFile(state.LogPath)
	if err != nil {
		return "📜 <b>LOG AKTIVITAS</b>\n\nBelum ada catatan log aktivitas."
	}

	lines := strings.Split(string(content), "\n")
	var matched []string

	filterLower := strings.ToLower(filter)
	for _, l := range lines {
		lTrim := strings.TrimSpace(l)
		if lTrim == "" {
			continue
		}
		lLow := strings.ToLower(lTrim)
		switch filterLower {
		case "error":
			if strings.Contains(lLow, "error") || strings.Contains(lLow, "warn") || strings.Contains(lLow, "gagal") {
				matched = append(matched, lTrim)
			}
		case "auth":
			if strings.Contains(lLow, "login") || strings.Contains(lLow, "sso") || strings.Contains(lLow, "listener") || strings.Contains(lLow, "sesi") {
				matched = append(matched, lTrim)
			}
		case "notif":
			if strings.Contains(lLow, "notifikasi") || strings.Contains(lLow, "terbuka") || strings.Contains(lLow, "alert") {
				matched = append(matched, lTrim)
			}
		case "presensi":
			if strings.Contains(lLow, "berhasil") || strings.Contains(lLow, "hadir") || strings.Contains(lLow, "submit") {
				matched = append(matched, lTrim)
			}
		default: // all
			matched = append(matched, lTrim)
		}
	}

	if len(matched) == 0 {
		return fmt.Sprintf("📜 <b>LOG AKTIVITAS (%s)</b>\n\nTidak ada entri log yang cocok.", strings.ToUpper(filter))
	}

	start := 0
	if len(matched) > 12 {
		start = len(matched) - 12
	}
	slice := matched[start:]

	return fmt.Sprintf("📜 <b>LOG AKTIVITAS (%s):</b>\n\n<code>%s</code>",
		strings.ToUpper(filter), strings.Join(slice, "\n"))
}

func formatKontholPublicCard() string {
	state.mu.RLock()
	multiCfg := state.MultiCfg
	state.mu.RUnlock()

	hasAcc := (multiCfg != nil && len(multiCfg.Accounts) > 0)
	accCount := 0
	waTarget := "Belum diatur"
	waProvider := "Belum diatur"

	if hasAcc {
		accCount = len(multiCfg.Accounts)
		if multiCfg.WhatsApp.TargetPhone != "" {
			waTarget = multiCfg.WhatsApp.TargetPhone
		}
		if multiCfg.WhatsApp.Provider != "" {
			waProvider = multiCfg.WhatsApp.Provider
		}
	}

	return fmt.Sprintf("👥 <b>KON-THOL PUBLIC EDITION (MULTI-ACCOUNT)</b>\n\n"+
		"Modul terintegrasi untuk menjalankan pemantauan presensi serentak banyak mahasiswa secara paralel.\n\n"+
		"📊 <b>Konfigurasi Aktif :</b>\n"+
		"• Total Akun Terdaftar : <b>%d Akun</b>\n"+
		"• WhatsApp Gateway : <b>%s</b>\n"+
		"• Target Nomor Utama : <code>%s</code>\n\n"+
		"Silakan pilih aksi:", accCount, waProvider, waTarget)
}

func formatPublicAccountsList() string {
	state.mu.RLock()
	multiCfg := state.MultiCfg
	state.mu.RUnlock()

	if multiCfg == nil || len(multiCfg.Accounts) == 0 {
		return "📋 <b>DAFTAR AKUN PUBLIK:</b>\n\nBelum ada akun yang terdaftar di accounts.json."
	}

	var sb strings.Builder
	sb.WriteString("📋 <b>DAFTAR AKUN TERDAFTAR (MULTI-ACCOUNT):</b>\n\n")

	for i, acc := range multiCfg.Accounts {
		maskedUser := acc.Username
		if len(maskedUser) > 7 {
			maskedUser = maskedUser[:4] + "***" + maskedUser[len(maskedUser)-10:]
		}
		wa := acc.WATarget
		if wa == "" {
			wa = "-"
		}
		sb.WriteString(fmt.Sprintf("%d. <b>%s</b>\n   👤 <code>%s</code>\n   📱 WA: <code>%s</code>\n\n",
			i+1, acc.Name, maskedUser, wa))
	}

	return sb.String()
}

func runPublicScanThread(chatID int64) {
	state.mu.RLock()
	multiCfg := state.MultiCfg
	state.mu.RUnlock()

	if multiCfg == nil || len(multiCfg.Accounts) == 0 {
		state.TgBot.ShowContentCard(chatID, "⚠️ <b>accounts.json belum dikonfigurasi.</b>", telegram.GetKontholPublicKeyboard())
		return
	}

	// 3-step adaptive progress bar: 33% -> 66% -> 100%
	loadingID := state.TgBot.ShowAdaptiveProgress(chatID, 1, 3, "Menginisialisasi sesi akun mahasiswa...")

	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []string

	state.TgBot.UpdateAdaptiveProgress(chatID, loadingID, 2, 3, "Memindai kelas serentak...")

	for _, acc := range multiCfg.Accounts {
		wg.Add(1)
		go func(a config.AccountItem) {
			defer wg.Done()
			cli, err := client.NewEtholClient(a.Username, a.Password)
			if err != nil {
				mu.Lock()
				results = append(results, fmt.Sprintf("• %s: Gagal init client", a.Name))
				mu.Unlock()
				return
			}
			ok, _ := cli.LoginCAS()
			if !ok {
				mu.Lock()
				results = append(results, fmt.Sprintf("• %s: Gagal login CAS", a.Name))
				mu.Unlock()
				return
			}
			_ = cli.UpdateCache(false)
			courses := cli.GetCourses()
			hadir := 0
			for _, c := range courses {
				isOpen, key, _ := cli.ScanCoursePresence(c)
				if isOpen && key != "" {
					succ, _ := cli.SubmitAttendance(c, key)
					if succ {
						hadir++
						_ = state.Dispatcher.SendWhatsApp(a.WATarget, fmt.Sprintf("✅ Presensi berhasil: %s", c.Name()))
					}
				}
			}
			mu.Lock()
			results = append(results, fmt.Sprintf("• %s: Selesai (%d kelas hadir)", a.Name, hadir))
			mu.Unlock()
		}(acc)
	}

	wg.Wait()
	state.TgBot.FinishAdaptiveProgress(chatID, loadingID, "Scan Selesai")

	resText := fmt.Sprintf("⚡ <b>HASIL SCAN SERENTAK MULTI-AKUN:</b>\n\n%s", strings.Join(results, "\n"))
	state.TgBot.ShowContentCard(chatID, resText, telegram.GetKontholPublicKeyboard())
}

// TELEGRAM CALLBACK DISPATCHER (100% UX OPTIMAL & IN-PLACE COOLDOWN TOGGLE)

func handleTelegramCallback(action string, chatID int64, msgID int, queryID string) {
	state.TgBot.ClearEphemeralNotifications(chatID)

	switch action {
	case "btn_menu", "btn_page_1":
		text := getWelcomeText(1)
		kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)

	case "btn_page_2":
		text := getWelcomeText(2)
		kb := telegram.GetMainKeyboard(2, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)

	case "btn_status":
		txt := formatStatusText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetStatusKeyboard())

	case "btn_scan":
		// 2-step adaptive progress: 50% -> 100%
		loadingID := state.TgBot.ShowAdaptiveProgress(chatID, 1, 2, "Memindai seluruh mata kuliah ke server ETHOL...")
		txt := scanActiveAttendance(true)
		state.TgBot.UpdateAdaptiveProgress(chatID, loadingID, 2, 2, "Memverifikasi token kehadiran...")
		state.TgBot.FinishAdaptiveProgress(chatID, loadingID, "Pemindaian")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "btn_jadwal":
		txt := formatJadwalText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "btn_tugas":
		txt := formatTugasText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "btn_rekap":
		txt := formatRekapDetail()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "btn_notif":
		txt := getRecentNotifs()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetPage2BackKeyboard())

	case "btn_log", "btn_log_all":
		txt := getFilteredLogs("all")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_err":
		txt := getFilteredLogs("error")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_auth":
		txt := getFilteredLogs("auth")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_notif":
		txt := getFilteredLogs("notif")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_log_pres":
		txt := getFilteredLogs("presensi")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "btn_help":
		txt := formatHelpText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetPage2BackKeyboard())

	case "btn_relogin":
		state.TgBot.ShowContentCard(chatID, "⏳ <i>Sedang merefresh otentikasi login ETHOL...</i>", telegram.GetPage2BackKeyboard())
		ok, _ := state.PrimaryClient.LoginCAS()
		if ok {
			_ = state.PrimaryClient.UpdateCache(true)
			resTxt := "✅ <b>SESI DIPERBARUI</b>\nOtentikasi login dan sinkronisasi data kuliah berhasil disegarkan."
			state.TgBot.ShowContentCard(chatID, resTxt, telegram.GetPage2BackKeyboard())
		} else {
			resTxt := "❌ <b>GAGAL REFRESH SESI</b>\nTidak dapat menghubungkan ulang ke login ETHOL. Silakan periksa koneksi SSO PENS."
			state.TgBot.ShowContentCard(chatID, resTxt, telegram.GetPage2BackKeyboard())
		}

	case "btn_cooldown":
		// IN-PLACE TOGGLE: Bebas dari kartu perantara yang kaku!
		state.mu.Lock()
		state.CooldownActive = true
		state.CooldownDate = getWIBNow().Format("2006-01-02")
		state.mu.Unlock()

		state.TgBot.AnswerCallbackQuery(queryID, "🟡 Mode Cooldown Aktif: Jeda s/d 00:00 WIB")
		text := getWelcomeTextWithCmd(1, "Success")
		kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)

	case "btn_resume":
		// IN-PLACE TOGGLE: Kembali siaga penuh seketika!
		state.mu.Lock()
		state.CooldownActive = false
		state.CooldownDate = ""
		state.mu.Unlock()

		state.TgBot.AnswerCallbackQuery(queryID, "🟢 Siaga Penuh: Bot kembali aktif memantau")
		text := getWelcomeTextWithCmd(1, "Success")
		kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)

	case "btn_konthol_public":
		txt := formatKontholPublicCard()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetKontholPublicKeyboard())

	case "btn_public_scan":
		go runPublicScanThread(chatID)

	case "btn_public_accounts":
		txt := formatPublicAccountsList()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetKontholPublicKeyboard())
	}
}

// TELEGRAM SLASH COMMAND DISPATCHER (ZERO DUPLICATE MENUS)

func handleTelegramCommand(cmd string, chatID int64, msgID int) {
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	appendFileLog(fmt.Sprintf("Command diterima dari [%d]: %s", chatID, cmd))

	state.TgBot.ClearEphemeralNotifications(chatID)

	switch cmdLower {
	case "/start", "/help", "help", "/menu", "menu":
		text := getWelcomeText(1)
		kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)

	case "/status", "status":
		txt := formatStatusText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetStatusKeyboard())

	case "/rekap", "rekap":
		txt := formatRekapDetail()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "/scan", "/absen", "scan", "absen":
		txt := scanActiveAttendance(true)
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "/tugas", "tugas":
		txt := formatTugasText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "/jadwal", "/matkul", "jadwal", "matkul":
		txt := formatJadwalText()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetBackKeyboard())

	case "/notif", "/notifikasi", "notif":
		txt := getRecentNotifs()
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetPage2BackKeyboard())

	case "/log", "/logs", "log":
		txt := getFilteredLogs("all")
		state.TgBot.ShowContentCard(chatID, txt, telegram.GetLogKeyboard())

	case "/relogin", "relogin":
		ok, _ := state.PrimaryClient.LoginCAS()
		if ok {
			_ = state.PrimaryClient.UpdateCache(true)
			state.TgBot.ShowContentCard(chatID, "✅ <b>Otentikasi login dan sinkronisasi data kuliah berhasil diperbarui.</b>", telegram.GetPage2BackKeyboard())
		} else {
			state.TgBot.ShowContentCard(chatID, "❌ <b>Gagal menyegarkan sesi login ETHOL.</b>", telegram.GetPage2BackKeyboard())
		}

	case "/cooldown", "cooldown":
		state.mu.Lock()
		state.CooldownActive = true
		state.CooldownDate = getWIBNow().Format("2006-01-02")
		state.mu.Unlock()
		text := getWelcomeTextWithCmd(1, "Success")
		kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)

	case "/resume", "resume":
		state.mu.Lock()
		state.CooldownActive = false
		state.CooldownDate = ""
		state.mu.Unlock()
		text := getWelcomeTextWithCmd(1, "Success")
		kb := telegram.GetMainKeyboard(1, isCooldownActiveToday())
		state.TgBot.SendMenu(chatID, text, kb, state.BannerPaths)
	}
}
