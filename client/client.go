package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"konthol/core_bridge"
)

type UserInfo struct {
	Nomor       int    `json:"nomor"`
	NipNrp      string `json:"nipnrp"`
	Nama        string `json:"nama"`
	Email       string `json:"email"`
	Status      string `json:"status"`
	Program     string `json:"program"`
	Jurusan     string `json:"jurusan"`
	JenisSchema int    `json:"jenis_schema"`
}

type EtholConfig struct {
	Tahun           int    `json:"tahun"`
	Semester        int    `json:"semester"`
	NamaSemester    string `json:"nama_semester"`
	TahunAjaranText string `json:"tahun_ajaran_text"`
}

type CourseItem struct {
	IDMatakuliah     int    `json:"id_matakuliah"`
	IDJadwalKuliah   int    `json:"id_jadwal_kuliah"`
	IDKelas          int    `json:"id_kelas"`
	IDKategori       int    `json:"id_kategori"`
	MataKuliah       string `json:"matakuliah"`
	NamaMataKuliah   string `json:"nama"`
	Kode             string `json:"kode"`
	Dosen            string `json:"dosen"`
	JumlahSKS        int    `json:"sks"`
	Ruang            string `json:"ruang"`
	Hari             string `json:"hari"`
	Jam              string `json:"jam"`
	JamMulai         string `json:"jam_mulai"`
	JamSelesai       string `json:"jam_selesai"`
	TotalPertemuan   int    `json:"total_pertemuan"`
	JumlahPresensi   int    `json:"jumlah_presensi"`
	PersenKehadiran  int    `json:"persen_kehadiran"`
}

func (c *CourseItem) Name() string {
	if c.MataKuliah != "" {
		return c.MataKuliah
	}
	return c.NamaMataKuliah
}

type ScheduleItem struct {
	Hari       string `json:"hari"`
	JamAwal    string `json:"jamAwal"`
	JamAkhir   string `json:"jamAkhir"`
	MataKuliah string `json:"matakuliah"`
	Nama       string `json:"nama"`
	Ruang      string `json:"ruang"`
	Dosen      string `json:"dosen"`
}

func (s *ScheduleItem) CourseName() string {
	if s.MataKuliah != "" {
		return s.MataKuliah
	}
	return s.Nama
}

type TaskItem struct {
	ID        int    `json:"id"`
	Judul     string `json:"judul"`
	Matkul    string `json:"matakuliah"`
	Deadline  string `json:"deadline"`
	Status    string `json:"status"`
	Deskripsi string `json:"deskripsi"`
}

type EtholClient struct {
	Username      string
	Password      string
	HTTPClient    *http.Client
	UserInfo      *UserInfo
	EtholConfig   *EtholConfig
	CoursesCache  []CourseItem
	ScheduleCache []ScheduleItem
	LastAuthTime  time.Time
	mu            sync.RWMutex
}

func NewEtholClient(username, password string) (*EtholClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("init cookie jar failed: %w", err)
	}

	transport := &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	return &EtholClient{
		Username: username,
		Password: password,
		HTTPClient: &http.Client{
			Jar:       jar,
			Transport: transport,
			Timeout:   35 * time.Second,
		},
	}, nil
}

func (c *EtholClient) LoginCAS() (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("[ETHOL] Melakukan otentikasi CAS SSO PENS untuk %s...", c.Username)

	// Cek apakah sesi yang tersimpan di cookies masih valid
	valid, info := c.checkTokenUnsafe()
	if valid {
		c.UserInfo = info
		c.LastAuthTime = time.Now()
		log.Printf("[ETHOL] Sesi CAS sudah aktif: %s (%s)", info.Nama, info.NipNrp)
		return true, nil
	}

	casRedirectURL := "https://ethol.pens.ac.id/api/auth/cas-redirect"
	req, err := http.NewRequest(http.MethodGet, casRedirectURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("cas-redirect request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read cas response failed: %w", err)
	}

	// Panggil Rust Core Parser via FFI
	rawAction, fields, err := core_bridge.ParseCASForm(string(bodyBytes))
	if err != nil || rawAction == "" {
		valid, info := c.checkTokenUnsafe()
		if valid {
			c.UserInfo = info
			c.LastAuthTime = time.Now()
			return true, nil
		}
		return false, fmt.Errorf("ekstraksi form CAS gagal dari Rust core: %v", err)
	}

	resolvedAction := rawAction
	if strings.HasPrefix(rawAction, "/") {
		resolvedAction = "https://login.pens.ac.id" + rawAction
	}

	formData := url.Values{}
	for k, v := range fields {
		formData.Set(k, v)
	}
	formData.Set("username", c.Username)
	formData.Set("password", c.Password)
	formData.Set("_eventId", "submit")
	formData.Set("submit", "LOGIN")
	formData.Del("reset")

	postReq, err := http.NewRequest(http.MethodPost, resolvedAction, strings.NewReader(formData.Encode()))
	if err != nil {
		return false, err
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	postResp, err := c.HTTPClient.Do(postReq)
	if err != nil {
		return false, fmt.Errorf("cas submit request failed: %w", err)
	}
	defer postResp.Body.Close()

	valid, info = c.checkTokenUnsafe()
	if !valid {
		return false, fmt.Errorf("validasi token gagal setelah login CAS")
	}

	c.UserInfo = info
	c.LastAuthTime = time.Now()
	log.Printf("[ETHOL] Login CAS Berhasil: %s (%s)", info.Nama, info.NipNrp)
	return true, nil
}

func (c *EtholClient) checkTokenUnsafe() (bool, *UserInfo) {
	valReq, err := http.NewRequest(http.MethodGet, "https://ethol.pens.ac.id/api/auth/validasi-token", nil)
	if err != nil {
		return false, nil
	}
	valReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	valResp, err := c.HTTPClient.Do(valReq)
	if err != nil || valResp.StatusCode != http.StatusOK {
		return false, nil
	}
	defer valResp.Body.Close()

	var info UserInfo
	if err := json.NewDecoder(valResp.Body).Decode(&info); err != nil {
		return false, nil
	}

	if info.Nomor > 0 || info.Nama != "" {
		return true, &info
	}
	return false, nil
}

func (c *EtholClient) UpdateCache(force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cfgURL := "https://ethol.pens.ac.id/api/konfigurasi-user"
	req, _ := http.NewRequest(http.MethodGet, cfgURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var cfg EtholConfig
		if err := json.NewDecoder(resp.Body).Decode(&cfg); err == nil {
			c.EtholConfig = &cfg
		}
	}

	tahun := time.Now().Year()
	smt := 1
	if c.EtholConfig != nil && c.EtholConfig.Tahun > 0 {
		tahun = c.EtholConfig.Tahun
		smt = c.EtholConfig.Semester
	}

	// Ambil Jadwal Kuliah
	schURL := fmt.Sprintf("https://ethol.pens.ac.id/api/jadwal-kuliah/mahasiswa?tahun=%d&semester=%d", tahun, smt)
	sReq, _ := http.NewRequest(http.MethodGet, schURL, nil)
	sReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	sResp, err := c.HTTPClient.Do(sReq)
	if err == nil {
		defer sResp.Body.Close()
		if sResp.StatusCode == http.StatusOK {
			var schs []ScheduleItem
			if err := json.NewDecoder(sResp.Body).Decode(&schs); err == nil {
				c.ScheduleCache = schs
			}
		}
	}

	// Ambil Mata Kuliah & Presensi
	crsURL := fmt.Sprintf("https://ethol.pens.ac.id/api/matakuliah-saya?tahun=%d&semester=%d", tahun, smt)
	cReq, _ := http.NewRequest(http.MethodGet, crsURL, nil)
	cReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	cResp, err := c.HTTPClient.Do(cReq)
	if err == nil {
		defer cResp.Body.Close()
		if cResp.StatusCode == http.StatusOK {
			var crss []CourseItem
			if err := json.NewDecoder(cResp.Body).Decode(&crss); err == nil {
				c.CoursesCache = crss
			}
		}
	}

	log.Printf("[ETHOL] Cache diperbarui: %d mata kuliah, %d jadwal aktif (Tahun %d, Sem %d)",
		len(c.CoursesCache), len(c.ScheduleCache), tahun, smt)
	return nil
}

func (c *EtholClient) GetCourses() []CourseItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	res := make([]CourseItem, len(c.CoursesCache))
	copy(res, c.CoursesCache)
	return res
}

func (c *EtholClient) GetSchedules() []ScheduleItem {
	c.mu.RLock()
	defer c.mu.RUnlock()
	res := make([]ScheduleItem, len(c.ScheduleCache))
	copy(res, c.ScheduleCache)
	return res
}

func (c *EtholClient) GetTasks() ([]TaskItem, error) {
	taskURL := "https://ethol.pens.ac.id/api/tugas-kuliah/mahasiswa/pending"
	req, _ := http.NewRequest(http.MethodGet, taskURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var tasks []TaskItem
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (c *EtholClient) ScanCoursePresence(course CourseItem) (bool, string, string) {
	urlCheck := fmt.Sprintf("https://ethol.pens.ac.id/api/presensi-kuliah/cek?id_kuliah=%d", course.IDMatakuliah)
	req, _ := http.NewRequest(http.MethodGet, urlCheck, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return false, "", ""
	}
	defer resp.Body.Close()

	var data struct {
		BukaPresensi bool   `json:"buka_presensi"`
		Key          string `json:"key"`
		IDPresensi   int    `json:"id_presensi"`
		Status       string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return false, "", ""
	}

	if data.BukaPresensi && data.Key != "" {
		return true, data.Key, fmt.Sprintf("%d", data.IDPresensi)
	}
	return false, "", ""
}

func (c *EtholClient) SubmitAttendance(course CourseItem, key string) (bool, string) {
	submitURL := "https://ethol.pens.ac.id/api/presensi-kuliah/mahasiswa"
	payload := map[string]interface{}{
		"id_kuliah": course.IDMatakuliah,
		"key":       key,
	}
	pBytes, _ := json.Marshal(payload)

	req, _ := http.NewRequest(http.MethodPost, submitURL, bytes.NewReader(pBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return true, "Presensi berhasil dicatat"
	}
	return false, string(body)
}
