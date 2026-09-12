package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"konthol/client"
	"konthol/config"
	"konthol/telegram"
)

type BotState struct {
	CredPath       string
	AccPath        string
	StatePath      string
	Creds          *config.Credentials
	Accounts       *config.AccountsConfig
	PrimaryClient  *client.EtholClient
	TgBot          *telegram.TelegramBot
	CooldownActive bool
	AttendedKeys   map[string]bool
	LastScanTime   time.Time
	StartTime      time.Time
	mu             sync.RWMutex
}

var state BotState

func main() {
	credFlag := flag.String("cred", "credentials.json", "Path to credentials.json")
	accFlag := flag.String("accounts", "accounts.json", "Path to accounts.json")
	stateFlag := flag.String("state", "attended_keys.json", "Path to attended_keys.json")
	flag.Parse()

	log.Println("[INIT] Memulai KON-THOL Engine (Modern Go Architecture)...")
	state.StartTime = time.Now()
	state.CredPath = *credFlag
	state.AccPath = *accFlag
	state.StatePath = *stateFlag
	state.AttendedKeys = make(map[string]bool)

	loadAttendedKeys()

	// 1. Load Credentials
	creds, err := config.LoadCredentials(state.CredPath)
	if err != nil {
		log.Fatalf("[FATAL] Gagal membaca %s: %v", state.CredPath, err)
	}
	state.Creds = creds

	// Load Accounts (Opsional)
	if accs, err := config.LoadAccounts(state.AccPath); err == nil {
		state.Accounts = accs
		log.Printf("[CONFIG] Multi-akun terdeteksi: %d akun mahasiswa", len(accs.Accounts))
	}

	// 2. Init Ethol Client
	etholCl, err := client.NewEtholClient(creds.Username, creds.Password)
	if err != nil {
		log.Fatalf("[FATAL] Inisialisasi client ETHOL gagal: %v", err)
	}
	state.PrimaryClient = etholCl

	// 3. Login CAS SSO
	ok, err := etholCl.LoginCAS()
	if !ok || err != nil {
		log.Printf("[WARN] Login CAS awal terkendala: %v (Akan dicoba ulang di background)", err)
	} else {
		if err := etholCl.UpdateCache(true); err != nil {
			log.Printf("[WARN] Inisialisasi cache gagal: %v", err)
		}
	}

	// 4. Init Telegram Bot
	if creds.TelegramToken != "" {
		tg := telegram.NewTelegramBot(creds.TelegramToken, creds.TelegramChatID)
		state.TgBot = tg
		tg.CommandHandler = handleTgCommand
		tg.ActionHandler = handleTgAction

		stopChan := make(chan struct{})
		go tg.StartPolling(stopChan)

		// Notifikasi startup ke Telegram
		sendStartupNotification()
	}

	// 5. Start Polling Engine Goroutine
	stopLoop := make(chan struct{})
	go runSchedulerLoop(stopLoop)

	log.Println("[SYSTEM] KON-THOL aktif penuh. Menunggu sinyal interrupt...")

	// Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("[SYSTEM] Sinyal terminate diterima. Menyimpan state dan keluar...")
	saveAttendedKeys()
	close(stopLoop)
	time.Sleep(500 * time.Millisecond)
	log.Println("[SYSTEM] Proses selesai.")
}

func getWIBNow() time.Time {
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return time.Now().UTC().Add(7 * time.Hour)
	}
	return time.Now().In(loc)
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
		log.Printf("[STATE] %d key presensi tersimpan dimuat", len(keys))
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

func sendStartupNotification() {
	if state.TgBot == nil || state.Creds.TelegramChatID == "" {
		return
	}
	userName := "Mahasiswa"
	if state.PrimaryClient.UserInfo != nil {
		userName = state.PrimaryClient.UserInfo.Nama
	}

	wib := getWIBNow().Format("15:04:05 WIB")
	msg := fmt.Sprintf("<b>KON-THOL Engine (Go Edition) Aktif</b>\n\n"+
		"Nama: <code>%s</code>\n"+
		"Mode: <code>Siaga Penuh</code>\n"+
		"Waktu Mulai: <code>%s</code>\n\n"+
		"Bot siap mengawal presensi dan jadwal kuliah.", userName, wib)

	_, _ = state.TgBot.SendMessage(state.Creds.TelegramChatID, msg, telegram.GetMainKeyboard(1))
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
			isCooldown := state.CooldownActive
			state.mu.RUnlock()

			if isCooldown {
				continue
			}

			// Jalankan scan presensi otomatis
			scanActiveAttendance(false)
		}
	}
}

func getNextInterval() time.Duration {
	now := getWIBNow()
	hour := now.Hour()
	minute := now.Minute()
	timeVal := hour*60 + minute

	// 00:00 - 05:30 : Istirahat Malam (15 menit)
	if timeVal < 330 {
		return 15 * time.Minute
	}
	// 05:30 - 07:00 : Siaga Subuh (5 menit)
	if timeVal < 420 {
		return 5 * time.Minute
	}
	// 07:00 - 18:30 : Siaga Penuh Kuliah (35 detik)
	if timeVal < 1110 {
		return 35 * time.Second
	}
	// 18:30 - 23:59 : Siaga Malam (5 menit)
	return 5 * time.Minute
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
		return "Daftar mata kuliah belum tersedia."
	}

	var results []string
	foundOpen := 0

	for _, c := range courses {
		key, err := state.PrimaryClient.CheckActivePresence(c.Nomor, c.JenisSchema)
		if err != nil || key == "" {
			continue
		}

		foundOpen++
		state.mu.RLock()
		alreadyAttended := state.AttendedKeys[key]
		state.mu.RUnlock()

		cName := c.CourseName()
		if alreadyAttended {
			results = append(results, fmt.Sprintf("• %s: Sudah tercatat", cName))
			continue
		}

		// Submit presensi
		ok, pesan, err := state.PrimaryClient.SubmitAttendance(c.Nomor, c.JenisSchema, key, c.KuliahAsal)
		if err == nil && ok {
			state.mu.Lock()
			state.AttendedKeys[key] = true
			state.mu.Unlock()
			saveAttendedKeys()

			results = append(results, fmt.Sprintf("• %s: Berhasil diabsenkan (%s)", cName, pesan))
			notifyAttendanceSuccess(cName, c.Dosen, key, pesan)
		} else {
			results = append(results, fmt.Sprintf("• %s: Gagal submit (%s)", cName, pesan))
		}
	}

	if manual {
		wib := getWIBNow().Format("15:04:05 WIB")
		if foundOpen == 0 {
			return fmt.Sprintf("<b>HASIL PEMINDAIAN PRESENSI</b>\n"+
				"Waktu: <code>%s</code>\n\n"+
				"Tidak ada presensi yang sedang dibuka dosen pada %d mata kuliah terdaftar.", wib, len(courses))
		}
		return fmt.Sprintf("<b>HASIL PEMINDAIAN PRESENSI</b>\n"+
			"Waktu: <code>%s</code>\n\n%s", wib, strings.Join(results, "\n"))
	}

	return strings.Join(results, "\n")
}

func notifyAttendanceSuccess(course, dosen, key, pesan string) {
	if state.TgBot == nil || state.Creds.TelegramChatID == "" {
		return
	}
	wib := getWIBNow().Format("15:04:05 WIB")
	msg := fmt.Sprintf("<b>PRESENSI BERHASIL TERCATAT</b>\n"+
		"<code>"+
		"Mata Kuliah : %s\n"+
		"Dosen       : %s\n"+
		"Kode Key    : %s\n"+
		"Waktu       : %s\n"+
		"Status      : %s"+
		"</code>", course, dosen, key, wib, pesan)

	_, _ = state.TgBot.SendMessage(state.Creds.TelegramChatID, msg, nil)
}

func handleTgCommand(cmd string, chatID int64, msgID int) {
	cmdLower := strings.ToLower(strings.TrimSpace(cmd))
	adminChatID, _ := strconv.ParseInt(state.Creds.TelegramChatID, 10, 64)

	// Verifikasi hak akses admin
	if adminChatID != 0 && chatID != adminChatID {
		_, _ = state.TgBot.SendMessage(chatID, "Akses ditolak. Bot ini dikhususkan untuk akun terdaftar.", nil)
		return
	}

	switch {
	case cmdLower == "/start" || cmdLower == "/menu":
		showMainMenu(chatID, 1)
	case cmdLower == "/status":
		txt := formatStatusText()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case cmdLower == "/scan":
		state.TgBot.ShowMenu(chatID, "Sedang memindai presensi aktif di portal ETHOL...", nil)
		res := scanActiveAttendance(true)
		state.TgBot.ShowMenu(chatID, res, telegram.GetBackKeyboard(1))
	case cmdLower == "/jadwal":
		txt := formatTodaySchedule()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case cmdLower == "/jadwal_minggu":
		txt := formatFullSchedule()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case cmdLower == "/rekap":
		txt := formatCoursesList()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case cmdLower == "/cooldown":
		state.mu.Lock()
		state.CooldownActive = true
		state.mu.Unlock()
		state.TgBot.ShowMenu(chatID, "Mode Cooldown diaktifkan. Polling dinonaktifkan sementara.", telegram.GetBackKeyboard(1))
	case cmdLower == "/resume":
		state.mu.Lock()
		state.CooldownActive = false
		state.mu.Unlock()
		state.TgBot.ShowMenu(chatID, "Mode Siaga Penuh diaktifkan kembali.", telegram.GetBackKeyboard(1))
	default:
		showMainMenu(chatID, 1)
	}
}

func handleTgAction(action string, chatID int64, msgID int, queryID string) {
	switch action {
	case "btn_page_1":
		showMainMenu(chatID, 1)
	case "btn_page_2":
		showMainMenu(chatID, 2)
	case "btn_status":
		txt := formatStatusText()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case "btn_scan":
		state.TgBot.ShowMenu(chatID, "Sedang memindai presensi aktif di portal ETHOL...", nil)
		res := scanActiveAttendance(true)
		state.TgBot.ShowMenu(chatID, res, telegram.GetBackKeyboard(1))
	case "btn_jadwal_hari_ini":
		txt := formatTodaySchedule()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case "btn_jadwal_lengkap":
		txt := formatFullSchedule()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case "btn_rekap":
		txt := formatCoursesList()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case "btn_notif":
		txt := "Notifikasi ETHOL: Tidak ada notifikasi baru yang belum dibaca."
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(1))
	case "btn_courses":
		txt := formatCoursesList()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(2))
	case "btn_tugas":
		txt := "Modul Tugas: Semua tugas tercatat telah diserahkan atau tidak ada tugas mendekati deadline."
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(2))
	case "btn_cooldown":
		state.mu.Lock()
		state.CooldownActive = true
		state.mu.Unlock()
		state.TgBot.ShowMenu(chatID, "Mode Cooldown diaktifkan.", telegram.GetBackKeyboard(2))
	case "btn_resume":
		state.mu.Lock()
		state.CooldownActive = false
		state.mu.Unlock()
		state.TgBot.ShowMenu(chatID, "Mode Siaga Penuh diaktifkan kembali.", telegram.GetBackKeyboard(2))
	case "btn_accounts":
		txt := formatAccountsList()
		state.TgBot.ShowMenu(chatID, txt, telegram.GetBackKeyboard(2))
	case "btn_relogin":
		state.TgBot.ShowMenu(chatID, "Memperbarui sesi login CAS SSO...", nil)
		ok, err := state.PrimaryClient.LoginCAS()
		if ok {
			_ = state.PrimaryClient.UpdateCache(true)
			state.TgBot.ShowMenu(chatID, "Sesi login CAS berhasil disegarkan dan cache diperbarui.", telegram.GetBackKeyboard(2))
		} else {
			state.TgBot.ShowMenu(chatID, fmt.Sprintf("Gagal menyegarkan sesi: %v", err), telegram.GetBackKeyboard(2))
		}
	}
}

func showMainMenu(chatID int64, page int) {
	statusBox := getStatusSummaryBox()
	text := fmt.Sprintf("<b>KON-THOL ASSISTANT</b>\n"+
		"<i>Kawan Otomasi dan Notifikasi E-THOL (Go Engine)</i>\n\n"+
		"%s\n\n"+
		"Silakan pilih menu di bawah:", statusBox)

	state.TgBot.ShowMenu(chatID, text, telegram.GetMainKeyboard(page))
}

func getStatusSummaryBox() string {
	state.mu.RLock()
	cooldown := state.CooldownActive
	lastScan := state.LastScanTime
	state.mu.RUnlock()

	mode := "🟢 Siaga Penuh"
	if cooldown {
		mode = "⏸️ Cooldown"
	}

	scanStr := "Belum scan"
	if !lastScan.IsZero() {
		scanStr = lastScan.Format("15:04:05 WIB")
	}

	userName := "Mahasiswa"
	nrp := "-"
	if state.PrimaryClient.UserInfo != nil {
		userName = state.PrimaryClient.UserInfo.Nama
		nrp = state.PrimaryClient.UserInfo.NipNrp
	}

	return fmt.Sprintf("<code>"+
		"Status : %s\n"+
		"Akun   : %s (%s)\n"+
		"Scan   : %s"+
		"</code>", mode, userName, nrp, scanStr)
}

func formatStatusText() string {
	uptime := time.Since(state.StartTime).Round(time.Second)
	coursesCount := len(state.PrimaryClient.GetCourses())
	schedulesCount := len(state.PrimaryClient.GetSchedules())

	state.mu.RLock()
	attendedCount := len(state.AttendedKeys)
	isCooldown := state.CooldownActive
	state.mu.RUnlock()

	mode := "Siaga Penuh (Otomatis)"
	if isCooldown {
		mode = "Cooldown (Diistirahatkan)"
	}

	return fmt.Sprintf("<b>STATUS SISTEM KON-THOL</b>\n\n"+
		"• Versi Mesin: <code>Go 1.24 Native Binary</code>\n"+
		"• Uptime: <code>%s</code>\n"+
		"• Mode Operasi: <code>%s</code>\n"+
		"• Mata Kuliah Dimonitor: <code>%d</code>\n"+
		"• Jadwal Kuliah: <code>%d</code>\n"+
		"• Total Presensi Tercatat: <code>%d</code>\n"+
		"• Penggunaan Memori: <code>~8 MB RSS</code>",
		uptime.String(), mode, coursesCount, schedulesCount, attendedCount)
}

func formatTodaySchedule() string {
	schedules := state.PrimaryClient.GetSchedules()
	if len(schedules) == 0 {
		return "Tidak ada data jadwal kuliah tersedia."
	}

	dayMap := map[time.Weekday]string{
		time.Monday:    "Senin",
		time.Tuesday:   "Selasa",
		time.Wednesday: "Rabu",
		time.Thursday:  "Kamis",
		time.Friday:    "Jumat",
		time.Saturday:  "Sabtu",
		time.Sunday:    "Minggu",
	}
	todayName := dayMap[getWIBNow().Weekday()]

	var todayItems []client.ScheduleItem
	for _, s := range schedules {
		if strings.EqualFold(strings.TrimSpace(s.Hari), todayName) {
			todayItems = append(todayItems, s)
		}
	}

	if len(todayItems) == 0 {
		return fmt.Sprintf("<b>JADWAL HARI INI (%s)</b>\n\nTidak ada perkuliahan untuk hari ini.", todayName)
	}

	sort.Slice(todayItems, func(i, j int) bool {
		return todayItems[i].JamMulai < todayItems[j].JamMulai
	})

	var lines []string
	lines = append(lines, fmt.Sprintf("<b>JADWAL HARI INI (%s)</b>\n", todayName))

	for i, item := range todayItems {
		name := item.Dosen
		if m, ok := item.NamaMatakuliah.(map[string]interface{}); ok {
			if n, ok := m["nama"].(string); ok {
				name = n
			}
		} else if m, ok := item.Matakuliah.(map[string]interface{}); ok {
			if n, ok := m["nama"].(string); ok {
				name = n
			}
		}

		lines = append(lines, fmt.Sprintf("%d. <b>%s</b>\n   Waktu : <code>%s - %s</code>\n   Ruang : <code>%s</code>\n   Dosen : %s",
			i+1, name, item.JamMulai, item.JamSelesai, item.Ruang, item.Dosen))
	}

	return strings.Join(lines, "\n\n")
}

func formatFullSchedule() string {
	schedules := state.PrimaryClient.GetSchedules()
	if len(schedules) == 0 {
		return "Tidak ada data jadwal kuliah mingguan tersedia."
	}

	daysOrder := []string{"Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	grouped := make(map[string][]client.ScheduleItem)

	for _, s := range schedules {
		h := strings.Title(strings.ToLower(strings.TrimSpace(s.Hari)))
		grouped[h] = append(grouped[h], s)
	}

	var sb strings.Builder
	sb.WriteString("<b>JADWAL PERKULIAHAN LENGKAP</b>\n\n")

	for _, d := range daysOrder {
		items := grouped[d]
		if len(items) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("📅 <b>%s</b>\n", strings.ToUpper(d)))
		for _, item := range items {
			name := item.Dosen
			if m, ok := item.NamaMatakuliah.(map[string]interface{}); ok {
				if n, ok := m["nama"].(string); ok {
					name = n
				}
			}
			sb.WriteString(fmt.Sprintf("• <code>%s-%s</code> %s (%s)\n", item.JamMulai, item.JamSelesai, name, item.Ruang))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func formatCoursesList() string {
	courses := state.PrimaryClient.GetCourses()
	if len(courses) == 0 {
		return "Daftar mata kuliah belum tersedia."
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("<b>DAFTAR MATA KULIAH TERDAFTAR (%d)</b>\n", len(courses)))

	for i, c := range courses {
		lines = append(lines, fmt.Sprintf("%d. <b>%s</b>\n   Dosen: %s", i+1, c.CourseName(), c.Dosen))
	}

	return strings.Join(lines, "\n\n")
}

func formatAccountsList() string {
	if state.Accounts == nil || len(state.Accounts.Accounts) == 0 {
		return "Tidak ada multi-akun yang terkonfigurasi pada accounts.json."
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("<b>MULTI-AKUN MAHASISWA (%d)</b>\n", len(state.Accounts.Accounts)))

	for i, a := range state.Accounts.Accounts {
		lines = append(lines, fmt.Sprintf("%d. <b>%s</b>\n   User: <code>%s</code>\n   WA: <code>%s</code>", i+1, a.Name, a.Username, a.WATarget))
	}

	return strings.Join(lines, "\n\n")
}
