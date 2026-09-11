#!/usr/bin/env python3
"""
KON-THOL (Kawan Otomasi dan Notifikasi untuk E-THOL PENS) - V1 Public Edition
Asisten cerdas presensi otomatis & informasi akademik PENS.
Mendukung Telegram Bot, Termux (Android), dan CLI Terminal (Windows/Linux).
"""

import os
import sys
import json
import time
import datetime
import logging
import threading
import argparse
import re
import html
import shutil
import subprocess
import urllib.parse
import requests
from bs4 import BeautifulSoup
from requests.adapters import HTTPAdapter
from urllib3.util import Retry

# Pastikan encoding output terminal mendukung UTF-8 di Windows/Linux/Termux
if sys.platform == "win32":
    try:
        sys.stdout.reconfigure(encoding='utf-8', errors='replace')
        sys.stderr.reconfigure(encoding='utf-8', errors='replace')
    except Exception:
        pass

# Zona Waktu Indonesia Barat (WIB / UTC+7)
WIB = datetime.timezone(datetime.timedelta(hours=7))

def get_wib_now():
    return datetime.datetime.now(WIB)

def get_wib_str():
    return get_wib_now().strftime("%Y-%m-%d %H:%M:%S WIB")

class WIBFormatter(logging.Formatter):
    def formatTime(self, record, datefmt=None):
        dt = datetime.datetime.fromtimestamp(record.created, WIB)
        return dt.strftime(datefmt or "%Y-%m-%d %H:%M:%S WIB")

# Deteksi lokasi file konfigurasi & log yang fleksibel (CWD, folder script, atau /opt)
BASE_DIR = os.path.dirname(os.path.abspath(__file__))

def resolve_file(filename):
    for candidate in [
        os.path.join(os.getcwd(), filename),
        os.path.join(BASE_DIR, filename),
        os.path.join("/opt/ethol-autopresence-public", filename),
        os.path.join("/opt/ethol-autopresence", filename)
    ]:
        if os.path.exists(candidate):
            return candidate
    return os.path.join(BASE_DIR, filename)

ACCOUNTS_FILE = resolve_file("accounts.json")

LOG_FILE = resolve_file("autopresence.log")
CRED_FILE = resolve_file("credentials.json")
STATE_FILE = resolve_file("attended_keys.json")

logger = logging.getLogger("KON-THOL")
logger.setLevel(logging.INFO)
try:
    fh = logging.FileHandler(LOG_FILE, encoding='utf-8')
    fh.setFormatter(WIBFormatter("%(asctime)s [%(levelname)s] %(message)s"))
    logger.addHandler(fh)
except Exception:
    pass

sh = logging.StreamHandler(sys.stdout)
sh.setFormatter(WIBFormatter("%(asctime)s [%(levelname)s] %(message)s"))
logger.addHandler(sh)

DAY_ORDER = {
    "senin": 1,
    "selasa": 2,
    "rabu": 3,
    "kamis": 4,
    "jumat": 5,
    "jum'at": 5,
    "sabtu": 6,
    "minggu": 7
}

def to_plain_text(html_text):
    clean = re.sub(r'</?(b|i|code|pre|em|strong|s|u)>', '', html_text)
    return html.unescape(clean)

def send_os_notification(title, message):
    """Kirim notifikasi lokal ke Termux (Android) atau Desktop Linux."""
    plain_msg = to_plain_text(message)
    if shutil.which("termux-notification"):
        try:
            subprocess.run([
                "termux-notification",
                "--title", title,
                "--content", plain_msg[:120],
                "--priority", "high"
            ], timeout=3, check=False)
            return
        except Exception:
            pass
    if shutil.which("notify-send"):
        try:
            subprocess.run(["notify-send", title, plain_msg[:120]], timeout=3, check=False)
            return
        except Exception:
            pass


class StudentAccount:
    """Representasi satu akun mahasiswa dengan sesi HTTP dan state presensi mandiri."""
    def __init__(self, name, username, password, wa_target="", telegram_chat_id=""):
        self.name = name or "Mahasiswa"
        self.username = username.strip()
        self.password = password.strip()
        self.wa_target = str(wa_target or "").strip()
        self.telegram_chat_id = str(telegram_chat_id or "").strip()

        self.session = requests.Session()
        retries = Retry(total=2, backoff_factor=0.3, status_forcelist=[500, 502, 503, 504])
        adapter = HTTPAdapter(max_retries=retries, pool_connections=5, pool_maxsize=10)
        self.session.mount("https://", adapter)
        self.session.mount("http://", adapter)
        self.session.headers.update({
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36',
            'Accept': 'application/json, text/plain, */*',
            'Accept-Language': 'id-ID,id;q=0.9,en-US;q=0.8,en;q=0.7',
            'Origin': 'https://ethol.pens.ac.id',
            'Referer': 'https://ethol.pens.ac.id/',
            'Sec-Ch-Ua': '"Chromium";v="128", "Not;A=Brand";v="24", "Google Chrome";v="128"',
            'Sec-Ch-Ua-Mobile': '?0',
            'Sec-Ch-Ua-Platform': '"Windows"',
        })

        self.user_info = None
        self.tahun_aktif = get_wib_now().year
        self.semester_aktif = 1
        self.courses_cache = []
        self.schedule_cache = []
        self.last_cache_update = 0
        self.last_auth_time = "-"

        # State presensi terisolasi per akun
        safe_user = "".join(c for c in self.username if c.isalnum()) or "user"
        self.state_file = resolve_file(f"attended_{safe_user}.json")
        self.attended_keys = set()
        self.load_attended_state()

    def load_attended_state(self):
        if os.path.exists(self.state_file):
            try:
                with open(self.state_file, 'r', encoding='utf-8') as f:
                    data = json.load(f)
                    self.attended_keys = set(data.get('attended_keys', []))
            except Exception as e:
                logger.warning(f"[{self.name}] Gagal memuat state: {e}")

    def save_attended_state(self):
        try:
            with open(self.state_file, 'w', encoding='utf-8') as f:
                json.dump({
                    "attended_keys": list(self.attended_keys),
                    "last_updated": get_wib_str()
                }, f, indent=2)
        except Exception as e:
            logger.warning(f"[{self.name}] Gagal menyimpan state: {e}")

    def login_cas(self, notify_on_fail=False):
        logger.info(f"[{self.name}] Memulai otentikasi CAS SSO PENS ({self.username})...")
        self.session.cookies.clear()
        try:
            resp = self.session.get('https://ethol.pens.ac.id/api/auth/cas-redirect', allow_redirects=True, timeout=15)
            soup = BeautifulSoup(resp.text, 'html.parser')
            form = soup.find('form', id='fm1')
            if not form:
                logger.error(f"[{self.name}] Form login CAS SSO tidak ditemukan.")
                return False

            action = form.get('action')
            post_url = urllib.parse.urljoin(resp.url, action)
            form_data = {inp.get('name'): inp.get('value', '') for inp in form.find_all('input') if inp.get('name')}
            form_data['username'] = self.username
            form_data['password'] = self.password
            form_data['_eventId'] = 'submit'
            form_data['submit'] = 'LOGIN'

            self.session.post(post_url, data=form_data, allow_redirects=True, timeout=15)

            val_resp = self.session.get('https://ethol.pens.ac.id/api/auth/validasi-token', timeout=10)
            if val_resp.status_code == 200:
                self.user_info = val_resp.json()
                self.last_auth_time = get_wib_str()
                logger.info(f"[{self.name}] Login Sukses: {self.user_info.get('nama')} ({self.user_info.get('nipnrp')})")
                self.update_cache(force=True)
                return True
            else:
                logger.error(f"[{self.name}] Validasi token gagal (HTTP {val_resp.status_code})")
                return False
        except Exception as e:
            logger.error(f"[{self.name}] Exception Login: {e}")
            return False

    def ensure_valid_session(self):
        if not self.user_info:
            return self.login_cas()
        try:
            r = self.session.post('https://ethol.pens.ac.id/api/auth/refresh', timeout=5)
            if r.status_code == 200:
                return True
        except Exception:
            pass
        return self.login_cas(notify_on_fail=False)

    def update_cache(self, force=False):
        now = time.time()
        if not force and (now - self.last_cache_update < 600) and self.courses_cache:
            return

        try:
            conf_resp = self.session.get('https://ethol.pens.ac.id/api/auth/config', timeout=8)
            if conf_resp.status_code == 200:
                c = conf_resp.json()
                self.tahun_aktif = c.get('tahun_aktif', self.tahun_aktif)
                self.semester_aktif = c.get('semester_aktif', self.semester_aktif)

            r_courses = self.session.get('https://ethol.pens.ac.id/api/kuliah', params={
                'tahun': self.tahun_aktif,
                'semester': self.semester_aktif
            }, timeout=10)
            if r_courses.status_code == 200:
                self.courses_cache = r_courses.json() or []

            r_jadwal = self.session.get('https://ethol.pens.ac.id/api/jadwal/jadwal-online', params={
                'tahun': self.tahun_aktif,
                'semester': self.semester_aktif
            }, timeout=10)
            if r_jadwal.status_code == 200:
                self.schedule_cache = r_jadwal.json() or []

            self.last_cache_update = now
        except Exception as e:
            logger.warning(f"[{self.name}] Gagal memperbarui cache data: {e}")

    def extract_active_key(self, pres_data):
        if not pres_data:
            return None
        if isinstance(pres_data, list) and len(pres_data) > 0:
            item = pres_data[0]
            if isinstance(item, dict):
                return item.get('key')
        elif isinstance(pres_data, dict):
            return pres_data.get('key')
        return None

    def get_active_course_now(self):
        now_dt = get_wib_now()
        day_map = {0: "senin", 1: "selasa", 2: "rabu", 3: "kamis", 4: "jumat", 5: "sabtu", 6: "minggu"}
        curr_day = day_map.get(now_dt.weekday(), "")

        if not self.schedule_cache or not self.courses_cache:
            return None

        for item in self.schedule_cache:
            h = str(item.get('hari', '')).lower().replace("'", "").strip()
            if h.startswith("jum"):
                h = "jumat"

            if h == curr_day:
                j_start = item.get('jam_awal', '')
                j_end = item.get('jam_akhir', '')
                if j_start and j_end:
                    try:
                        h_s, m_s = map(int, j_start.split(':'))
                        h_e, m_e = map(int, j_end.split(':'))
                        start_min = (h_s * 60 + m_s) - 15
                        end_min = (h_e * 60 + m_e) + 20
                        cur_min = now_dt.hour * 60 + now_dt.minute

                        if start_min <= cur_min <= end_min:
                            k_id = item.get('kuliah') or item.get('nomor') or item.get('id_kuliah')
                            for c in self.courses_cache:
                                mk_obj = c.get('nama_matakuliah') or c.get('matakuliah')
                                mk_name = mk_obj.get('nama') if isinstance(mk_obj, dict) else (mk_obj or "")
                                if c.get('nomor') == k_id or (item.get('matakuliah') and item.get('matakuliah') in mk_name):
                                    return c
                    except Exception:
                        pass
        return None

    def scan_and_attend(self, notify_callback=None, manual=False):
        now_wib = get_wib_now()
        now_str = get_wib_str()
        today_str = now_wib.strftime("%Y-%m-%d")

        if not self.ensure_valid_session():
            return f"❌ <b>[{self.name}]</b> Gagal otentikasi SSO PENS."

        self.update_cache()
        if not self.courses_cache:
            return f"⚠️ <b>[{self.name}]</b> Data mata kuliah kosong atau gagal dimuat."

        found_open = 0
        results = []

        active_course = self.get_active_course_now()
        courses_to_scan = list(self.courses_cache)
        if active_course:
            k_act_id = active_course.get('nomor')
            courses_to_scan = [c for c in courses_to_scan if c.get('nomor') == k_act_id] + [c for c in courses_to_scan if c.get('nomor') != k_act_id]

        for c in courses_to_scan:
            k_id = c.get('nomor')
            schema = c.get('jenis_schema') or c.get('jenisSchema') or 0
            mk_obj = c.get('nama_matakuliah') or c.get('matakuliah')
            mk_nama = mk_obj.get('nama') if isinstance(mk_obj, dict) else (mk_obj or f"Kuliah #{k_id}")
            dosen = c.get('dosen') or "Dosen Pengampu"
            kuliah_asal = c.get('kuliah_asal') or k_id

            try:
                pres_resp = self.session.get(
                    'https://ethol.pens.ac.id/api/presensi/aktif-kuliah',
                    params={'kuliah': k_id, 'jenis_schema': schema},
                    timeout=8
                )

                if pres_resp.status_code == 401:
                    if self.login_cas():
                        pres_resp = self.session.get(
                            'https://ethol.pens.ac.id/api/presensi/aktif-kuliah',
                            params={'kuliah': k_id, 'jenis_schema': schema},
                            timeout=8
                        )
                    else:
                        continue

                if pres_resp.status_code == 200:
                    pres_data = pres_resp.json()
                    key = self.extract_active_key(pres_data)

                    if key:
                        found_open += 1
                        unique_today_key = f"{today_str}_{key}"
                        if key in self.attended_keys or unique_today_key in self.attended_keys:
                            results.append(f"ℹ️ <b>[{self.name}] {mk_nama}</b>: Sudah tercatat hadir.")
                            continue

                        logger.info(f"[{self.name}] ⚡ Presensi Terbuka Ditemukan: {mk_nama} (Key: {key})")
                        payload = {
                            "kuliah": k_id,
                            "jenis_schema": schema,
                            "mahasiswa": self.user_info.get('nomor') if self.user_info else None,
                            "key": key,
                            "kuliah_asal": kuliah_asal
                        }
                        submit_resp = self.session.post('https://ethol.pens.ac.id/api/presensi/mahasiswa', json=payload, timeout=10)

                        if submit_resp.status_code == 401:
                            if self.login_cas():
                                payload["mahasiswa"] = self.user_info.get('nomor') if self.user_info else None
                                submit_resp = self.session.post('https://ethol.pens.ac.id/api/presensi/mahasiswa', json=payload, timeout=10)

                        if submit_resp.status_code == 200:
                            res_json = submit_resp.json()
                            pesan = res_json.get('pesan') or res_json.get('message') or 'Berhasil'
                            is_success = (
                                res_json.get('sukses') or
                                res_json.get('success') or
                                res_json.get('status') in [200, "200", True] or
                                "sudah" in str(pesan).lower() or
                                "berhasil" in str(pesan).lower()
                            )

                            if is_success:
                                mhs_nama = self.user_info.get('nama') if self.user_info else self.name
                                nrp_str = f" ({self.user_info.get('nipnrp')})" if self.user_info and self.user_info.get('nipnrp') else ""
                                success_msg = (
                                    "🎉 <b>PRESENSI BERHASIL DICATAT!</b>\n\n"
                                    f"👤 <b>Mahasiswa:</b> {mhs_nama}{nrp_str}\n"
                                    f"📚 <b>Mata Kuliah:</b> {mk_nama}\n"
                                    f"👨‍🏫 <b>Dosen:</b> {dosen}\n"
                                    f"🔑 <b>Key:</b> <code>{key}</code>\n"
                                    f"🕒 <b>Waktu:</b> {now_str}\n"
                                    f"💬 <b>Respon:</b> {pesan}"
                                )
                                logger.info(f"[{self.name}] Berhasil hadir: {mk_nama} - {pesan}")
                                if notify_callback:
                                    notify_callback(self, success_msg, is_success=True)
                                self.attended_keys.add(key)
                                self.attended_keys.add(unique_today_key)
                                self.save_attended_state()
                                results.append(f"✅ <b>[{self.name}] {mk_nama}</b>: {pesan}")
                            else:
                                msg = f"⚠️ [{self.name}] {mk_nama}: {pesan}"
                                logger.warning(msg)
                                if notify_callback:
                                    notify_callback(self, msg, is_success=False)
                                results.append(msg)
                        else:
                            err = f"❌ [{self.name}] Gagal kirim presensi {mk_nama} (HTTP {submit_resp.status_code})"
                            logger.error(err)
                            results.append(err)
            except Exception as e:
                logger.error(f"[{self.name}] Error parse presensi {mk_nama}: {e}")

        if manual:
            if found_open == 0:
                return f"✅ <b>[{self.name}]</b> Tidak ada presensi terbuka ({len(self.courses_cache)} mata kuliah)."
            return "\n".join(results)
        return "Scan selesai."


    def get_attendance_statistics(self):
        self.update_cache()
        nomor_mhs = self.user_info.get('nomor') if self.user_info else None
        if not nomor_mhs:
            return None

        today_date_str = get_wib_now().strftime("%d-%m-%Y")
        total_dosen_semester = 0
        total_mhs_semester = 0
        total_dosen_today = 0
        total_mhs_today = 0
        course_breakdown = []

        for c in self.courses_cache:
            k_id = c.get('nomor')
            schema = c.get('jenis_schema') or c.get('jenisSchema') or 0
            mk = c.get('nama_matakuliah') or c.get('matakuliah')
            mk_name = mk.get('nama') if isinstance(mk, dict) else mk
            dosen_nomor = c.get('nomor_dosen')

            try:
                mhs_resp = self.session.get(
                    'https://ethol.pens.ac.id/api/presensi/riwayat',
                    params={'kuliah': k_id, 'jenis_schema': schema, 'nomor': nomor_mhs},
                    timeout=5
                )
                mhs_list = mhs_resp.json() if mhs_resp.status_code == 200 and isinstance(mhs_resp.json(), list) else []

                dosen_resp = self.session.get(
                    'https://ethol.pens.ac.id/api/presensi/get-tanggal-presensi-dosen-per-semester',
                    params={'tahun': self.tahun_aktif, 'semester': self.semester_aktif, 'kuliah': k_id, 'dosen': dosen_nomor},
                    timeout=5
                )
                dosen_list = dosen_resp.json() if dosen_resp.status_code == 200 and isinstance(dosen_resp.json(), list) else []

                d_today = sum(1 for d in dosen_list if today_date_str in str(d.get('waktu_indonesia', '')) or today_date_str in str(d.get('waktu', '')))
                m_today = sum(1 for m in mhs_list if today_date_str in str(m.get('tanggal', '')) or today_date_str in str(m.get('waktu_indonesia', '')))

                total_dosen_semester += len(dosen_list)
                total_mhs_semester += len(mhs_list)
                total_dosen_today += d_today
                total_mhs_today += m_today

                course_breakdown.append({
                    "kuliah_id": k_id,
                    "nama": mk_name,
                    "hadir": len(mhs_list),
                    "total": len(dosen_list),
                    "d_today": d_today,
                    "m_today": m_today
                })
            except Exception:
                pass

        pct = 100.0 if total_dosen_semester == 0 else (total_mhs_semester / total_dosen_semester) * 100.0
        return {
            "percentage": pct,
            "total_dosen_semester": total_dosen_semester,
            "total_mhs_semester": total_mhs_semester,
            "total_dosen_today": total_dosen_today,
            "total_mhs_today": total_mhs_today,
            "breakdown": course_breakdown
        }

    def get_pending_tasks(self):
        self.update_cache()
        pending = []
        for c in self.courses_cache:
            k_id = c.get('nomor')
            schema = c.get('jenis_schema') or c.get('jenisSchema') or 0
            mk = c.get('nama_matakuliah') or c.get('matakuliah')
            mk_name = mk.get('nama') if isinstance(mk, dict) else mk

            try:
                res = self.session.get('https://ethol.pens.ac.id/api/tugas', params={'kuliah': k_id, 'jenisSchema': schema}, timeout=5)
                if res.status_code == 200:
                    tasks = res.json()
                    if isinstance(tasks, list):
                        for t in tasks:
                            if not t.get('submission_time') and str(t.get('tutup', '0')) != "1":
                                pending.append({
                                    "kuliah_id": k_id,
                                    "matkul": mk_name,
                                    "title": t.get('title') or t.get('judul'),
                                    "deadline": t.get('deadline_indonesia') or t.get('deadline') or "-"
                                })
            except Exception:
                pass
        return pending

    def format_status_text(self, bot_ref=None):
        nama = self.user_info.get('nama', self.name) if self.user_info else self.name
        nrp = self.user_info.get('nipnrp', 'N/A') if self.user_info else '-'
        now_str = get_wib_str()

        now_wib = get_wib_now()
        time_val = now_wib.hour + now_wib.minute / 60.0

        is_cooldown = bot_ref.is_cooldown_active_today() if bot_ref else False
        force_siaga = bot_ref.force_siaga if bot_ref else False
        cooldown_date = bot_ref.cooldown_date if bot_ref else ""

        if is_cooldown:
            scanner_status = f"🟡 Cooldown ({cooldown_date})"
            scanner_sub = "💤 Jeda s/d 00:00 WIB"
            jadwal_relogin = "Auto re-login esok hari (00:00 WIB)"
            aktivitas = "Istirahat (monitoring jeda)"
        elif force_siaga:
            scanner_status = "🟢 Siaga Penuh (Override Manual)"
            scanner_sub = "• Memantau aktif (Istirahat Malam di-bypass)"
            jadwal_relogin = "Pengecekan sesi berkala"
            aktivitas = "Siaga penuh memantau presensi malam"
        elif time_val >= 21.5 or time_val < 4.0:
            scanner_status = "💤 Istirahat Malam"
            scanner_sub = "• Jeda malam (dosen offline)"
            jadwal_relogin = "Siaga subuh (04:00 WIB)"
            aktivitas = "Standby malam (gunakan /resume jika ada kuliah)"
        elif 4.0 <= time_val < 6.5:
            scanner_status = "🌅 Siaga Subuh"
            scanner_sub = "• Memantau persiapan kuliah pagi"
            jadwal_relogin = "Pengecekan sesi berkala"
            aktivitas = "Siaga subuh menyambut jadwal kuliah"
        else:
            scanner_status = "🟢 Siaga Penuh"
            scanner_sub = "• Standby memantau presensi"
            jadwal_relogin = "Pengecekan sesi berkala"
            aktivitas = "Siaga memantau presensi & jadwal"

        tg_status = f"🟢 Terhubung (<code>{self.telegram_chat_id}</code>)" if self.telegram_chat_id else "⚪ Belum Ditautkan"

        return (
            "<b>┌─ DATA MAHASISWA ─────────────────</b>\n"
            f"│ Mahasiswa      : {nama}\n"
            f"│ NRP            : <code>{nrp}</code>\n"
            f"│ Telegram       : {tg_status}\n"
            f"│ Waktu Server   : {now_str}\n"
            "<b>├─ SESI LOGIN & RE-LOGIN ───────────</b>\n"
            "│ Sesi Login     : 🟢 Terhubung (Aktif)\n"
            f"│ Terakhir Login : <code>{self.last_auth_time}</code>\n"
            f"│ Jadwal Re-login: {jadwal_relogin}\n"
            "<b>├─ OPERASIONAL SCANNER ────────────</b>\n"
            f"│ Status Scanner : {scanner_status}\n"
            f"│                  {scanner_sub}\n"
            f"│ Aktivitas      : {aktivitas}\n"
            "<b>└──────────────────────────────────</b>"
        )

    def format_rekap_detail(self):
        stats = self.get_attendance_statistics()
        mhs_nama = self.user_info.get('nama', self.name) if self.user_info else self.name
        if not stats:
            return f"Gagal memuat rekapitulasi kehadiran untuk <b>{mhs_nama}</b> dari server ETHOL."

        now_wib = get_wib_now()
        today_idx = now_wib.weekday()
        day_names = {0: "senin", 1: "selasa", 2: "rabu", 3: "kamis", 4: "jumat", 5: "sabtu", 6: "minggu"}
        today_day_clean = day_names.get(today_idx, "")

        def clean_day(d):
            return str(d or '').lower().replace("'", "").replace("`", "").strip()

        courses_scheduled_today = set()
        for item in self.schedule_cache:
            if clean_day(item.get('hari')) == today_day_clean:
                k_id = item.get('nomor') or item.get('kuliah')
                mk_name = item.get('matakuliah')
                if k_id: courses_scheduled_today.add(k_id)
                if mk_name: courses_scheduled_today.add(str(mk_name))

        txt = (
            f"<b>REKAPITULASI KEHADIRAN: {mhs_nama}</b>\n\n"
            f"• Rata-rata Total : <b>{stats['percentage']:.1f}%</b>\n"
            f"• Total Kehadiran : {stats['total_mhs_semester']} dari {stats['total_dosen_semester']} sesi perkuliahan\n"
            f"• Hadir Hari Ini  : {stats['total_mhs_today']} sesi tervalidasi hadir\n\n"
            "<b>RINCIAN PER MATA KULIAH:</b>\n\n"
        )

        for item in stats['breakdown']:
            mk_name = item['nama']
            k_id = item.get('kuliah_id')
            d_today = item.get('d_today', 0)
            m_today = item.get('m_today', 0)
            hadir_sem = item.get('hadir', 0)

            is_today = (k_id in courses_scheduled_today or mk_name in courses_scheduled_today)

            if is_today:
                if m_today > 0:
                    status_sesi = f"🟢 <code>[Sesi Selesai: Tervalidasi Hadir ({m_today} Sesi)]</code>"
                elif d_today > 0:
                    status_sesi = "⚠️ <code>[Sesi Terbuka: Belum Hadir]</code>"
                else:
                    status_sesi = "⚪ <code>[Belum Ada Sesi Dibuka Dosen]</code>"

                txt += (
                    f"• <b>{mk_name}</b> (Hari Ini)\n"
                    f"  Status Sesi : {status_sesi}\n"
                    f"  Total Hadir : {hadir_sem} kali pertemuan dalam semester ini.\n\n"
                )
            elif m_today > 0 or d_today > 0:
                txt += (
                    f"• <b>{mk_name}</b> (Luar Jadwal)\n"
                    f"  Status Sesi : 🟠 <code>[Sesi Luar Jadwal: Hadir ({m_today} Sesi)]</code>\n"
                    f"  Total Hadir : {hadir_sem} kali pertemuan dalam semester ini.\n\n"
                )
            else:
                txt += (
                    f"• <b>{mk_name}</b>\n"
                    f"  Total Hadir : {hadir_sem} kali pertemuan dalam semester ini.\n\n"
                )
        return txt

    def format_tugas_text(self):
        tasks = self.get_pending_tasks()
        mhs_nama = self.user_info.get('nama', self.name) if self.user_info else self.name
        if not tasks:
            return f"<b>DAFTAR TUGAS KULIAH ({mhs_nama})</b>\n\nSemua tugas semester ini telah dikumpulkan atau tidak ada tugas aktif."

        txt = f"<b>DAFTAR TUGAS PENDING: {mhs_nama}</b>\n\n"
        links_dict = {}

        for idx, t in enumerate(tasks, 1):
            k_id = t.get('kuliah_id')
            mk = t.get('matkul', 'Mata Kuliah')
            if k_id and mk not in links_dict:
                links_dict[mk] = f"https://ethol.pens.ac.id/mahasiswa/matakuliah/{k_id}/tugas"

            txt += (
                f"<b>{idx}. {t['title']}</b>\n"
                f"   Mata Kuliah : {mk}\n"
                f"   Tenggat     : <code>{t['deadline']}</code>\n\n"
            )

        if len(links_dict) == 1:
            _, url_tugas = next(iter(links_dict.items()))
            txt += f"Tautan Web : {url_tugas}\n"
        else:
            txt += "<b>Tautan Web Pengumpulan:</b>\n"
            for mk_name, url_tugas in links_dict.items():
                txt += f"• {mk_name} :\n  {url_tugas}\n"

        txt += (
            "\n⚠️ <i>Catatan: Harap pastikan Anda sudah login ke akun ETHOL di browser "
            "terlebih dahulu sebelum membuka tautan di atas agar dapat langsung diarahkan ke tugas tersebut.</i>"
        )
        return txt

    def format_jadwal_text(self):
        self.update_cache(force=True)
        mhs_nama = self.user_info.get('nama', self.name) if self.user_info else self.name
        if not self.schedule_cache:
            return f"Data jadwal perkuliahan untuk <b>{mhs_nama}</b> belum tersedia."

        now_wib = get_wib_now()
        today_idx = now_wib.weekday()
        day_names = {0: "senin", 1: "selasa", 2: "rabu", 3: "kamis", 4: "jumat", 5: "sabtu", 6: "minggu"}
        today_day_clean = day_names.get(today_idx, "")

        def clean_day(d):
            return str(d or '').lower().replace("'", "").replace("`", "").strip()

        def get_day_val(item):
            if 'nomor_hari' in item and item['nomor_hari']:
                return item['nomor_hari']
            return DAY_ORDER.get(clean_day(item.get('hari', '')), 99)

        sorted_jadwal = sorted(self.schedule_cache, key=lambda x: (get_day_val(x), x.get('jam_awal', '00:00')))

        txt = f"<b>JADWAL KULIAH: {mhs_nama}</b> (Sem {self.semester_aktif}/{self.tahun_aktif}):\n"
        curr_day = ""

        for item in sorted_jadwal:
            d_raw = str(item.get('hari', '') or '').strip()
            if not d_raw or d_raw.lower() == "none":
                d_raw = "Lainnya"
            d_clean = clean_day(d_raw)

            if d_raw != curr_day:
                curr_day = d_raw
                tag = " (HARI INI)" if d_clean == today_day_clean else ""
                txt += f"\n🗓️ <b>[{curr_day.upper()}{tag}]</b>\n"

            jam_awal = item.get('jam_awal', '-')
            jam_akhir = item.get('jam_akhir', '-')
            mk = item.get('matakuliah', '-')
            dosen = item.get('dosen') or "Dosen Pengampu"
            ruang = item.get('ruang') or "Online"

            jam_str = "Fleksibel" if not jam_awal or jam_awal == "-" else f"{jam_awal} - {jam_akhir}"

            txt += (
                f"• <b>{mk}</b>\n"
                f"  ⏰ <code>{jam_str} WIB</code> • 📍 {ruang}\n"
                f"  👨‍🏫 <i>{dosen}</i>\n\n"
            )
        return txt

class EtholBot:
    def __init__(self):
        if not os.path.exists(CRED_FILE):
            print(f"[!] File credentials.json tidak ditemukan!")
            print(f"    Salin config.example.json menjadi credentials.json dan isi akun PENS Anda.")
            sys.exit(1)

        with open(CRED_FILE, 'r', encoding='utf-8') as f:
            creds = json.load(f)

        self.username = creds['username']
        self.password = creds['password']
        self.tg_token = creds.get('telegram_token', '')
        self.tg_chat_id = str(creds.get('telegram_chat_id', ''))

        self.session = requests.Session()
        retries = Retry(total=2, backoff_factor=0.3, status_forcelist=[500, 502, 503, 504])
        adapter = HTTPAdapter(max_retries=retries, pool_connections=10, pool_maxsize=20)
        self.session.mount("https://", adapter)
        self.session.mount("http://", adapter)

        self.session.headers.update({
            'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36',
            'Accept': 'application/json, text/plain, */*',
            'Accept-Language': 'id-ID,id;q=0.9,en-US;q=0.8,en;q=0.7',
            'Origin': 'https://ethol.pens.ac.id',
            'Referer': 'https://ethol.pens.ac.id/',
            'Sec-Ch-Ua': '"Chromium";v="128", "Not;A=Brand";v="24", "Google Chrome";v="128"',
            'Sec-Ch-Ua-Mobile': '?0',
            'Sec-Ch-Ua-Platform': '"Windows"',
        })

        self.user_info = None
        self.tahun_aktif = get_wib_now().year
        self.semester_aktif = 1
        self.courses_cache = []
        self.schedule_cache = []
        self.last_cache_update = 0

        self.cooldown_date = None
        self.attended_keys = set()
        self.load_attended_state()

        self.processed_notif_ids = set()
        self.last_scan_time = "-"
        self.last_auth_time = "-"
        self.lock = threading.Lock()
        self.chat_main_menu = {}
        self.chat_last_interaction = {}
        self.force_siaga = False
        self.current_mode = None
        self.accounts = []
        self.wa_config = {}
        self.load_accounts()

    def load_attended_state(self):
        if os.path.exists(STATE_FILE):
            try:
                with open(STATE_FILE, 'r', encoding='utf-8') as f:
                    data = json.load(f)
                    self.attended_keys = set(data.get('attended_keys', []))
                    self.cooldown_date = data.get('cooldown_date')
            except Exception as e:
                logger.warning(f"Gagal memuat state: {e}")

    def save_attended_state(self):
        try:
            with open(STATE_FILE, 'w', encoding='utf-8') as f:
                json.dump({
                    "attended_keys": list(self.attended_keys),
                    "cooldown_date": self.cooldown_date,
                    "last_updated": get_wib_str()
                }, f, indent=2)
        except Exception as e:
            logger.warning(f"Gagal menyimpan state: {e}")


    def load_accounts(self):
        self.accounts = []
        self.wa_config = {}
        acc_file = resolve_file("accounts.json")
        if os.path.exists(acc_file):
            try:
                with open(acc_file, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    accs = data.get("accounts", [])
                    self.wa_config = data.get("whatsapp", {})
                    for item in accs:
                        acc = StudentAccount(
                            name=item.get("name", "Mahasiswa"),
                            username=item.get("username", ""),
                            password=item.get("password", ""),
                            wa_target=item.get("wa_target", ""),
                            telegram_chat_id=item.get("telegram_chat_id", "")
                        )
                        self.accounts.append(acc)
            except Exception as e:
                logger.error(f"Gagal membaca accounts.json: {e}")

        # Fallback jika accounts.json belum ada
        if not self.accounts:
            primary_name = "Utama"
            if self.user_info and self.user_info.get("nama"):
                primary_name = self.user_info.get("nama")
            self.accounts.append(StudentAccount(
                name=primary_name,
                username=self.username,
                password=self.password,
                telegram_chat_id=self.tg_chat_id
            ))

        # Pastikan sesi dan profil mahasiswa tersinkronkan
        for acc in self.accounts:
            if not acc.user_info:
                try:
                    acc.ensure_valid_session()
                except Exception as e:
                    logger.warning(f"[{acc.name}] Gagal sinkronisasi sesi awal: {e}")

    def get_account_by_chat_id(self, chat_id):
        cid = str(chat_id or "").strip()
        if not cid:
            return None
        for acc in self.accounts:
            if acc.telegram_chat_id and acc.telegram_chat_id == cid:
                return acc
        if self.tg_chat_id and cid == self.tg_chat_id and self.accounts:
            return self.accounts[0]
        return None

    def is_admin(self, chat_id):
        cid = str(chat_id or "").strip()
        if not cid:
            return False
        if self.tg_chat_id and cid == self.tg_chat_id:
            return True
        if self.accounts and self.accounts[0].telegram_chat_id and cid == self.accounts[0].telegram_chat_id:
            return True
        return False

    def set_telegram_id(self, target, tg_id):
        acc_file = resolve_file("accounts.json")
        if not os.path.exists(acc_file):
            return False, "Berkas accounts.json belum ada."
        try:
            with open(acc_file, "r", encoding="utf-8") as f:
                data = json.load(f)
            accs = data.get("accounts", [])
            target_acc = None
            if target.isdigit():
                idx = int(target) - 1
                if 0 <= idx < len(accs):
                    target_acc = accs[idx]
            else:
                for a in accs:
                    if a.get("username", "").lower() == target.lower():
                        target_acc = a
                        break
            if not target_acc:
                return False, f"Akun '{target}' tidak ditemukan."

            target_acc["telegram_chat_id"] = str(tg_id).strip()
            with open(acc_file, "w", encoding="utf-8") as f:
                json.dump(data, f, indent=4)
            self.load_accounts()
            return True, f"Telegram ID akun <b>{target_acc.get('name')}</b> berhasil diatur ke <code>{tg_id}</code>."
        except Exception as e:
            return False, f"Gagal mengupdate telegram ID: {e}"

    def bind_telegram_account(self, chat_id, email, password):
        chat_id_str = str(chat_id).strip()
        email_str = email.strip()
        acc_file = resolve_file("accounts.json")

        if os.path.exists(acc_file):
            try:
                with open(acc_file, "r", encoding="utf-8") as f:
                    data = json.load(f)
                accs = data.get("accounts", [])
                for a in accs:
                    if a.get("username", "").lower() == email_str.lower():
                        if a.get("password") != password:
                            test_acc = StudentAccount(name=a.get("name"), username=email_str, password=password)
                            if not test_acc.login_cas():
                                return False, "Password tidak cocok dengan akun SSO PENS Anda."
                            a["password"] = password

                        a["telegram_chat_id"] = chat_id_str
                        with open(acc_file, "w", encoding="utf-8") as f:
                            json.dump(data, f, indent=4)
                        self.load_accounts()
                        matched_acc = self.get_account_by_chat_id(chat_id_str)
                        return True, matched_acc
            except Exception as e:
                return False, f"Gagal membaca konfigurasi: {e}"

        test_acc = StudentAccount(name="Mahasiswa", username=email_str, password=password, telegram_chat_id=chat_id_str)
        if not test_acc.login_cas():
            return False, "Login ke SSO PENS gagal. Periksa kembali email dan password Anda."

        m_name = test_acc.user_info.get("nama") if test_acc.user_info else "Mahasiswa"
        succ, info = self.add_account(m_name, email_str, password, tg_id=chat_id_str)
        if not succ:
            return False, info
        matched_acc = self.get_account_by_chat_id(chat_id_str)
        return True, matched_acc

    def send_whatsapp(self, phone, message):
        if not phone or not self.wa_config:
            return False
        provider = self.wa_config.get("provider", "fonnte").lower()
        api_key = self.wa_config.get("api_key", "")
        endpoint = self.wa_config.get("endpoint_url", "https://api.fonnte.com/send")
        try:
            if provider == "fonnte":
                headers = {"Authorization": api_key}
                payload = {"target": phone, "message": message, "countryCode": "62"}
                r = requests.post(endpoint, headers=headers, data=payload, timeout=8)
                return r.status_code in [200, 201]
            elif provider in ["generic", "webhook", "wppconnect"]:
                headers = {"Content-Type": "application/json"}
                if api_key:
                    headers["Authorization"] = f"Bearer {api_key}"
                payload = {"phone": phone, "message": message}
                r = requests.post(endpoint, headers=headers, json=payload, timeout=8)
                return r.status_code in [200, 201]
        except Exception as e:
            logger.error(f"Gagal kirim WhatsApp ke {phone}: {e}")
        return False

    def notify_attendance(self, student_acc, text, is_success=True):
        self.send_tg(text)
        if student_acc.telegram_chat_id and student_acc.telegram_chat_id != self.tg_chat_id:
            try:
                url = f"https://api.telegram.org/bot{self.tg_token}/sendMessage"
                requests.post(url, json={"chat_id": student_acc.telegram_chat_id, "text": text, "parse_mode": "HTML"}, timeout=6)
            except Exception:
                pass
        title = "Presensi Berhasil!" if is_success else "Status Presensi"
        send_os_notification(title, f"[{student_acc.name}] {to_plain_text(text)[:100]}")
        wa_target = student_acc.wa_target or self.wa_config.get("target_phone")
        if wa_target:
            self.send_whatsapp(wa_target, to_plain_text(text))

    def format_accounts_list(self):
        if not self.accounts:
            return "📋 <b>DAFTAR AKUN MAHASISWA:</b>\n\nBelum ada akun terdaftar."

        txt = (
            "👥 <b>DAFTAR AKUN KON-THOL PUBLIK</b>\n"
            f"<i>Total Terdaftar: {len(self.accounts)} Akun Mahasiswa</i>\n\n"
        )
        for idx, acc in enumerate(self.accounts, 1):
            if not acc.user_info:
                acc.ensure_valid_session()
            user = acc.username
            if "@" in user:
                u_p, d_p = user.split("@", 1)
                masked_user = (u_p[:3] + "***@" + d_p) if len(u_p) > 3 else user
            else:
                masked_user = (user[:3] + "***") if len(user) > 3 else user

            tag = " (Akun Utama)" if idx == 1 else ""
            nrp_str = f"NRP: {acc.user_info.get('nipnrp')}" if acc.user_info and acc.user_info.get('nipnrp') else "Belum sinkron"
            tg_str = f"🟢 Terhubung (<code>{acc.telegram_chat_id}</code>)" if acc.telegram_chat_id else "⚪ Belum Ditautkan"

            now_wib = get_wib_now()
            time_val = now_wib.hour + now_wib.minute / 60.0

            if not acc.user_info:
                status_str = "🔴 Terputus (Perlu login ulang)"
            elif self.is_cooldown_active_today():
                status_str = f"🟡 Cooldown / Standby ({nrp_str})"
            elif self.force_siaga:
                status_str = f"🟢 Siaga Penuh [Override] ({nrp_str})"
            elif time_val >= 21.5 or time_val < 4.0:
                status_str = f"💤 Istirahat Malam ({nrp_str})"
            elif 4.0 <= time_val < 6.5:
                status_str = f"🌅 Siaga Subuh ({nrp_str})"
            else:
                status_str = f"🟢 Terhubung ({nrp_str})"

            txt += (
                f"<b>{idx}. {acc.name}</b>{tag}\n"
                f"   • Email    : <code>{masked_user}</code>\n"
                f"   • Status   : {status_str}\n"
                f"   • Telegram : {tg_str}\n\n"
            )

        txt += (
            "💡 <b>Panduan Kelola Akun Telegram:</b>\n"
            "• Tambah akun : <code>/addaccount Nama | email | password [| telegram_id]</code>\n"
            "• Tautkan Telegram : <code>/settelegram email_atau_nomor telegram_id</code>\n"
            "• Hapus akun  : <code>/delaccount email_atau_nomor</code>\n"
            "• Scan semua  : <code>/scanall</code>"
        )
        return txt

    def add_account(self, name, username, password, wa_target="", tg_id=""):
        test_acc = StudentAccount(name=name, username=username, password=password, wa_target=wa_target, telegram_chat_id=tg_id)
        if not test_acc.login_cas():
            return False, "Kredensial ditolak oleh SSO PENS (Email atau Password salah)."

        acc_file = resolve_file("accounts.json")
        acc_data = {"whatsapp": self.wa_config, "accounts": []}
        if os.path.exists(acc_file):
            try:
                with open(acc_file, "r", encoding="utf-8") as f:
                    acc_data = json.load(f)
            except Exception:
                pass
        else:
            if self.accounts:
                prim = self.accounts[0]
                acc_data["accounts"].append({
                    "name": prim.name,
                    "username": prim.username,
                    "password": prim.password,
                    "wa_target": prim.wa_target,
                    "telegram_chat_id": prim.telegram_chat_id
                })

        accs = acc_data.setdefault("accounts", [])
        for a in accs:
            if a.get("username", "").lower() == username.lower():
                a["name"] = name
                a["password"] = password
                a["wa_target"] = wa_target
                break
        else:
            accs.append({
                "name": name,
                "username": username,
                "password": password,
                "wa_target": wa_target,
                "telegram_chat_id": tg_id
            })

        try:
            with open(acc_file, "w", encoding="utf-8") as f:
                json.dump(acc_data, f, indent=4)
        except Exception as e:
            return False, f"Gagal menulis accounts.json: {e}"

        self.load_accounts()
        for a in self.accounts:
            if a.username.lower() == username.lower():
                a.user_info = test_acc.user_info
                a.session = test_acc.session
                a.last_auth_time = test_acc.last_auth_time
                break
        mhs_name = test_acc.user_info.get('nama', name) if test_acc.user_info else name
        nrp = test_acc.user_info.get('nipnrp', '') if test_acc.user_info else ''
        return True, f"Mahasiswa: <b>{mhs_name}</b> (NRP: <code>{nrp}</code>)"

    def del_account(self, target):
        acc_file = resolve_file("accounts.json")
        if not os.path.exists(acc_file):
            return False, "Berkas accounts.json belum ada."

        try:
            with open(acc_file, "r", encoding="utf-8") as f:
                data = json.load(f)
            accs = data.get("accounts", [])
            if not accs:
                return False, "Tidak ada akun terdaftar dalam accounts.json."

            target_str = str(target).strip()
            removed_name = None
            if target_str.isdigit() and int(target_str) <= len(accs):
                idx = int(target_str) - 1
                if 0 <= idx < len(accs):
                    removed_name = accs[idx].get("name")
                    del accs[idx]

            if not removed_name:
                for idx, a in enumerate(accs):
                    if a.get("username", "").lower() == target_str.lower() or str(a.get("telegram_chat_id", "")) == target_str:
                        removed_name = a.get("name")
                        del accs[idx]
                        break

            if not removed_name:
                return False, f"Akun dengan email, urutan, atau Telegram ID '{target_str}' tidak ditemukan."

            data["accounts"] = accs
            with open(acc_file, "w", encoding="utf-8") as f:
                json.dump(data, f, indent=4)

            self.load_accounts()
            return True, f"Akun <b>{removed_name}</b> berhasil dihapus dari daftar monitoring."
        except Exception as e:
            return False, f"Gagal menghapus akun: {e}"

    def is_cooldown_active_today(self):
        return self.cooldown_date == get_wib_now().strftime("%Y-%m-%d")

    def activate_cooldown(self):
        today = get_wib_now().strftime("%Y-%m-%d")
        self.force_siaga = False
        if self.cooldown_date == today:
            return False, f"Mode Cooldown sudah aktif untuk hari ini ({today})."
        self.cooldown_date = today
        self.save_attended_state()
        self.current_mode = "COOLDOWN"
        self.update_telegram_menu_ui()
        return True, f"Mode Cooldown aktif untuk hari ini ({today}). Pemantauan otomatis diistirahatkan hingga esok hari agar hemat daya."

    def deactivate_cooldown(self):
        self.cooldown_date = None
        self.force_siaga = True
        self.save_attended_state()
        self.current_mode = "FORCE_SIAGA"
        self.update_telegram_menu_ui()
        return True, "Mode Siaga Penuh diaktifkan! Mode Cooldown / Istirahat Malam dibatalkan. Bot aktif memantau presensi secara berkala."

    def get_banner_path(self):
        for candidate in [
            resolve_file(os.path.join("assets", "banner.jpg")),
            resolve_file(os.path.join("assets", "banner.png")),
            resolve_file("banner.jpg"),
            resolve_file("banner.png"),
        ]:
            if os.path.exists(candidate):
                return candidate
        return None

    def send_tg_photo(self, caption, photo_path=None, chat_id=None):
        target_chat = str(chat_id or self.tg_chat_id).strip()
        if not self.tg_token or not target_chat:
            return None
        p = photo_path or self.get_banner_path()
        if p and os.path.exists(p) and len(caption) <= 1024:
            try:
                url = f"https://api.telegram.org/bot{self.tg_token}/sendPhoto"
                with open(p, 'rb') as photo:
                    r = requests.post(url, data={
                        "chat_id": target_chat,
                        "caption": caption,
                        "parse_mode": "HTML"
                    }, files={"photo": photo}, timeout=12)
                    if r.status_code == 200:
                        data = r.json()
                        if data.get('ok'):
                            return data['result'].get('message_id')
            except Exception as e:
                logger.error(f"Gagal kirim banner Telegram: {e}")
        return self.send_tg(caption, chat_id=target_chat)

    def send_tg(self, text, chat_id=None):
        target_chat = str(chat_id or self.tg_chat_id).strip()
        if not self.tg_token or not target_chat:
            return None
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/sendMessage"
            r = requests.post(url, json={
                "chat_id": target_chat,
                "text": text,
                "parse_mode": "HTML"
            }, timeout=8)
            if r.status_code == 200:
                data = r.json()
                if data.get('ok'):
                    return data['result'].get('message_id')
        except Exception as e:
            logger.error(f"Gagal kirim Telegram ke {target_chat}: {e}")
        return None

    def delete_tg_message(self, message_id, chat_id=None):
        target_chat = str(chat_id or self.tg_chat_id).strip()
        if not self.tg_token or not target_chat or not message_id:
            return
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/deleteMessage"
            requests.post(url, json={
                "chat_id": target_chat,
                "message_id": message_id
            }, timeout=5)
        except Exception:
            pass

    def delete_tg_messages(self, message_ids, chat_id=None):
        if not message_ids:
            return
        for mid in message_ids:
            if mid:
                self.delete_tg_message(mid, chat_id=chat_id)

    def start_loading_bar(self, text="Memproses...", chat_id=None):
        target_chat = str(chat_id or self.tg_chat_id).strip()
        if not self.tg_token or not target_chat:
            return None
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/sendMessage"
            payload = {
                "chat_id": target_chat,
                "text": f"⏳ <b>{html.escape(text)}</b>\n<code>[▰▰▰▰▰▱▱▱▱▱] 50%</code>",
                "parse_mode": "HTML"
            }
            r = requests.post(url, json=payload, timeout=6)
            if r.status_code == 200:
                return r.json().get('result', {}).get('message_id')
        except Exception as e:
            logger.debug(f"Gagal kirim loading bar: {e}")
        return None

    def advance_loading_bar(self, loading_id, text="Menyiapkan data...", chat_id=None):
        target_chat = str(chat_id or self.tg_chat_id).strip()
        if not self.tg_token or not target_chat or not loading_id:
            return
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/editMessageText"
            payload = {
                "chat_id": target_chat,
                "message_id": loading_id,
                "text": f"⚡ <b>{html.escape(text)}</b>\n<code>[▰▰▰▰▰▰▰▰▰▰] 100%</code>",
                "parse_mode": "HTML"
            }
            requests.post(url, json=payload, timeout=5)
        except Exception:
            pass

    def finish_loading_bar(self, loading_id, chat_id=None):
        target_chat = str(chat_id or self.tg_chat_id).strip()
        if not self.tg_token or not target_chat or not loading_id:
            return
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/deleteMessage"
            payload = {"chat_id": target_chat, "message_id": loading_id}
            requests.post(url, json=payload, timeout=5)
        except Exception:
            pass

    def get_status_box(self, student_acc=None):
        now_wib = get_wib_now()
        now_time_str = now_wib.strftime("%H:%M")
        time_val = now_wib.hour + now_wib.minute / 60.0

        user_info = student_acc.user_info if student_acc else self.user_info

        if not user_info:
            return (
                "<code>┌─ STATUS ────────────\n"
                "│ 🔴 Server Terputus\n"
                "│ ⚠️ Butuh /relogin\n"
                "└─────────────────────</code>"
            )

        if self.is_cooldown_active_today():
            return (
                "<code>┌─ STATUS ────────────\n"
                "│ 🟡 Mode Cooldown\n"
                "│ 💤 Jeda s/d 00:00 WIB\n"
                "└─────────────────────</code>"
            )

        if self.force_siaga:
            return (
                "<code>┌─ STATUS ────────────\n"
                "│ 🟢 Siaga Penuh\n"
                f"│ 🕒 {now_time_str} WIB (Override)\n"
                "└─────────────────────</code>"
            )

        if time_val >= 21.5 or time_val < 4.0:
            return (
                "<code>┌─ STATUS ────────────\n"
                "│ 💤 Istirahat Malam\n"
                f"│ 🕒 {now_time_str} (Standby)\n"
                "└─────────────────────</code>"
            )

        if 4.0 <= time_val < 6.5:
            return (
                "<code>┌─ STATUS ────────────\n"
                "│ 🌅 Siaga Subuh\n"
                f"│ 🕒 {now_time_str} (Standby)\n"
                "└─────────────────────</code>"
            )

        return (
            "<code>┌─ STATUS ────────────\n"
            "│ 🟢 Siaga Penuh\n"
            f"│ 🕒 {now_time_str} WIB (SSO OK)\n"
            "└─────────────────────</code>"
        )

    def format_menu_text(self, student_acc=None, is_admin=False):
        status_box = self.get_status_box(student_acc)
        credit = "\n\n✦ <b>Creator : Gungna</b>"
        mhs_name = student_acc.name if student_acc else "Mahasiswa"
        nrp_str = f" ({student_acc.user_info.get('nipnrp')})" if student_acc and student_acc.user_info and student_acc.user_info.get('nipnrp') else ""

        if is_admin:
            multi_note = f"\n👥 <i>Mode Master Agregasi Aktif: {len(self.accounts)} Mahasiswa Terdaftar</i>" if len(self.accounts) > 1 else ""
            return (
                f"👑 <b>KON-THOL ASSISTANT — MASTER ADMIN CONTROL</b>\n"
                f"<i>Akun Utama: {mhs_name}{nrp_str}</i>{multi_note}\n\n"
                f"{status_box}\n\n"
                "<b>PANDUAN MASTER DASHBOARD (ADMIN):</b>\n"
                "📊 /rekap [all | 1..N] - Rekapitulasi agregat / per mahasiswa\n"
                "📅 /jadwal [all | 1..N] - Agenda hari ini agregat / per mahasiswa\n"
                "📝 /tugas [all | 1..N] - Rekap tugas pending multi-mahasiswa\n"
                "ℹ️ /status - Dashboard operasional & sesi multi-akun\n"
                "⚡ /scanall - Scan serentak seluruh mahasiswa\n"
                "⚡ /scan [1..N] - Scan presensi akun aktif / target\n"
                "🔄 /relogin - Sinkronisasi sesi SSO PENS\n\n"
                "<b>PANDUAN KONTROL SISTEM & AKUN:</b>\n"
                "👥 /accounts - Daftar status akun & Telegram ID\n"
                "➕ /addaccount - Tambah mahasiswa baru\n"
                "🔗 /settelegram - Tautkan ID Telegram mahasiswa\n"
                "➖ /delaccount - Hapus akun (berdasarkan email / urutan)\n"
                "💤 /cooldown & ⚡ /resume - Kontrol jadwal scanner\n"
                "📜 /log - Riwayat catatan log aktivitas"
                + credit
            )
        else:
            return (
                f"<b>KON-THOL ASSISTANT</b>\n"
                f"<i>Akun Anda: {mhs_name}{nrp_str}</i>\n\n"
                f"{status_box}\n\n"
                "<b>PANDUAN PERINTAH:</b>\n"
                "⚡ /scan atau /absen - Scan presensi perkuliahan Anda\n"
                "📅 /jadwal - Jadwal perkuliahan mingguan Anda\n"
                "📊 /rekap - Rekapitulasi kehadiran resmi semester\n"
                "📝 /tugas - Daftar tugas pending & tautan pengumpulan\n"
                "ℹ️ /status - Status akun & sesi login SSO PENS\n"
                "🔄 /relogin - Sinkronisasi ulang sesi SSO Anda"
                + credit
            )

    def edit_tg_caption(self, message_id, caption):
        if not self.tg_token or not self.tg_chat_id or not message_id:
            return False
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/editMessageCaption"
            r = requests.post(url, json={
                "chat_id": self.tg_chat_id,
                "message_id": message_id,
                "caption": caption,
                "parse_mode": "HTML"
            }, timeout=8)
            return r.status_code == 200
        except Exception:
            return False

    def edit_tg_text(self, message_id, text):
        if not self.tg_token or not self.tg_chat_id or not message_id:
            return False
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/editMessageText"
            r = requests.post(url, json={
                "chat_id": self.tg_chat_id,
                "message_id": message_id,
                "text": text,
                "parse_mode": "HTML"
            }, timeout=8)
            return r.status_code == 200
        except Exception:
            return False

    def update_telegram_menu_ui(self):
        admin_chat = self.tg_chat_id
        menu_id = self.chat_main_menu.get(admin_chat)
        if menu_id and self.tg_token:
            acc = self.get_account_by_chat_id(admin_chat)
            menu_text = self.format_menu_text(student_acc=acc, is_admin=True)
            self.send_tg_photo(menu_text, chat_id=admin_chat)

    def determine_mode(self, now_wib=None):
        if not now_wib:
            now_wib = get_wib_now()
        time_val = now_wib.hour + now_wib.minute / 60.0
        if self.is_cooldown_active_today():
            return "COOLDOWN"
        if self.force_siaga:
            return "FORCE_SIAGA"
        if time_val >= 21.5 or time_val < 4.0:
            return "ISTIRAHAT_MALAM"
        if 4.0 <= time_val < 6.5:
            return "SIAGA_SUBUH"
        return "SIAGA_NORMAL"

    def notify_mode_change(self, old_mode, new_mode):
        now_time_str = get_wib_now().strftime("%H:%M")
        if new_mode == "ISTIRAHAT_MALAM":
            msg = (
                "🌙 <b>PERGANTIAN MODE: ISTIRAHAT MALAM</b>\n"
                f"🕒 Waktu: <code>{now_time_str} WIB</code>\n\n"
                "Sistem memasuki periode istirahat malam (21:30 - 04:00 WIB). "
                "Pemantauan santai tetap aktif berkala untuk mengantisipasi presensi malam dadakan. "
                "Gunakan perintah /resume atau /siaga jika ada perkuliahan malam."
            )
        elif new_mode == "SIAGA_SUBUH":
            msg = (
                "🌅 <b>PERGANTIAN MODE: SIAGA SUBUH</b>\n"
                f"🕒 Waktu: <code>{now_time_str} WIB</code>\n\n"
                "Waktu subuh telah tiba (04:00 - 06:30 WIB). Bot mengaktifkan Siaga Subuh dan menyegarkan sesi SSO "
                "untuk bersiap menyambut pembukaan presensi kuliah pagi hari ini!"
            )
        elif new_mode in ["SIAGA_NORMAL", "FORCE_SIAGA"]:
            msg = (
                "🟢 <b>PERGANTIAN MODE: SIAGA PENUH</b>\n"
                f"🕒 Waktu: <code>{now_time_str} WIB</code>\n\n"
                "Jam operasional perkuliahan aktif. Bot kembali siaga penuh memantau presensi dan jadwal perkuliahan!"
            )
        elif new_mode == "COOLDOWN":
            msg = (
                "🟡 <b>MODE COOLDOWN AKTIF</b>\n"
                f"🕒 Waktu: <code>{now_time_str} WIB</code>\n\n"
                "Pemantauan otomatis diistirahatkan hingga pergantian hari (00:00 WIB) untuk menghemat daya."
            )
        else:
            return
        self.send_tg(msg)

    def login_cas(self, notify_on_fail=False):
        logger.info("Memulai otentikasi CAS SSO PENS...")
        self.session.cookies.clear()
        try:
            resp = self.session.get('https://ethol.pens.ac.id/api/auth/cas-redirect', allow_redirects=True, timeout=15)
            soup = BeautifulSoup(resp.text, 'html.parser')
            form = soup.find('form', id='fm1')
            if not form:
                err = "Form login CAS SSO tidak ditemukan."
                logger.error(err)
                if notify_on_fail: self.send_tg(f"❌ <b>[ERROR LOGIN]</b> {err}")
                return False

            action = form.get('action')
            post_url = urllib.parse.urljoin(resp.url, action)
            form_data = {inp.get('name'): inp.get('value', '') for inp in form.find_all('input') if inp.get('name')}
            form_data['username'] = self.username
            form_data['password'] = self.password
            form_data['_eventId'] = 'submit'
            form_data['submit'] = 'LOGIN'

            self.session.post(post_url, data=form_data, allow_redirects=True, timeout=15)

            val_resp = self.session.get('https://ethol.pens.ac.id/api/auth/validasi-token', timeout=10)
            if val_resp.status_code == 200:
                self.user_info = val_resp.json()
                self.last_auth_time = get_wib_str()
                logger.info(f"Login Sukses: {self.user_info.get('nama')} ({self.user_info.get('nipnrp')})")
                self.update_cache(force=True)
                return True
            else:
                err = f"Validasi token gagal (HTTP {val_resp.status_code})"
                logger.error(err)
                if notify_on_fail: self.send_tg(f"⚠️ <b>[ERROR SSO]</b> {err}")
                return False
        except Exception as e:
            logger.error(f"Exception Login: {e}")
            if notify_on_fail: self.send_tg(f"❌ <b>[EXCEPTION LOGIN]</b> {e}")
            return False

    def ensure_valid_session(self):
        if not self.user_info:
            return self.login_cas()
        try:
            r = self.session.post('https://ethol.pens.ac.id/api/auth/refresh', timeout=5)
            if r.status_code == 200:
                return True
        except Exception:
            pass
        return self.login_cas(notify_on_fail=False)

    def update_cache(self, force=False):
        now = time.time()
        if not force and (now - self.last_cache_update < 600) and self.courses_cache:
            return

        try:
            conf_resp = self.session.get('https://ethol.pens.ac.id/api/auth/config', timeout=8)
            if conf_resp.status_code == 200:
                c = conf_resp.json()
                self.tahun_aktif = c.get('tahun_aktif', self.tahun_aktif)
                self.semester_aktif = c.get('semester_aktif', self.semester_aktif)

            r_courses = self.session.get('https://ethol.pens.ac.id/api/kuliah', params={
                'tahun': self.tahun_aktif,
                'semester': self.semester_aktif
            }, timeout=10)
            if r_courses.status_code == 200:
                self.courses_cache = r_courses.json() or []

            r_jadwal = self.session.get('https://ethol.pens.ac.id/api/jadwal/jadwal-online', params={
                'tahun': self.tahun_aktif,
                'semester': self.semester_aktif
            }, timeout=10)
            if r_jadwal.status_code == 200:
                self.schedule_cache = r_jadwal.json() or []

            self.last_cache_update = now
        except Exception as e:
            logger.warning(f"Gagal memperbarui cache data: {e}")

    def extract_active_key(self, pres_data):
        if not pres_data:
            return None
        if isinstance(pres_data, list) and len(pres_data) > 0:
            item = pres_data[0]
            if isinstance(item, dict):
                return item.get('key')
        elif isinstance(pres_data, dict):
            return pres_data.get('key')
        return None

    def get_active_course_now(self):
        """Mendeteksi mata kuliah yang sedang aktif berlangsung saat ini sesuai jadwal."""
        now_dt = get_wib_now()
        day_map = {0: "senin", 1: "selasa", 2: "rabu", 3: "kamis", 4: "jumat", 5: "sabtu", 6: "minggu"}
        curr_day = day_map.get(now_dt.weekday(), "")

        if not self.schedule_cache or not self.courses_cache:
            return None

        for item in self.schedule_cache:
            h = str(item.get('hari', '')).lower().replace("'", "").strip()
            if h.startswith("jum"):
                h = "jumat"

            if h == curr_day:
                j_start = item.get('jam_awal', '')
                j_end = item.get('jam_akhir', '')
                if j_start and j_end:
                    try:
                        h_s, m_s = map(int, j_start.split(':'))
                        h_e, m_e = map(int, j_end.split(':'))
                        # Toleransi: 15 menit sebelum kelas s/d 20 menit setelah jam berakhir
                        start_min = (h_s * 60 + m_s) - 15
                        end_min = (h_e * 60 + m_e) + 20
                        cur_min = now_dt.hour * 60 + now_dt.minute

                        if start_min <= cur_min <= end_min:
                            k_id = item.get('kuliah') or item.get('nomor') or item.get('id_kuliah')
                            for c in self.courses_cache:
                                mk_obj = c.get('nama_matakuliah') or c.get('matakuliah')
                                mk_name = mk_obj.get('nama') if isinstance(mk_obj, dict) else (mk_obj or "")
                                if c.get('nomor') == k_id or (item.get('matakuliah') and item.get('matakuliah') in mk_name):
                                    return c
                    except Exception:
                        pass
        return None

    def scan_and_attend(self, manual=False):
        now_wib = get_wib_now()
        now_str = get_wib_str()
        today_str = now_wib.strftime("%Y-%m-%d")
        self.last_scan_time = now_str

        # Jika terdapat multi-account di self.accounts
        if len(self.accounts) > 1:
            all_results = []
            for acc in self.accounts:
                res = acc.scan_and_attend(notify_callback=self.notify_attendance, manual=manual)
                if manual:
                    all_results.append(res)
                time.sleep(1)
            if manual:
                return "📋 <b>HASIL PEMINDAIAN MULTI-AKUN:</b>\n\n" + "\n\n".join(all_results)
            return "Scan multi-akun selesai."

        if not self.ensure_valid_session():
            return "❌ Gagal mengautentikasi ke SSO PENS."

        self.update_cache()
        if not self.courses_cache:
            return "⚠️ Data mata kuliah kosong atau gagal dimuat."

        found_open = 0
        results = []

        # Prioritaskan mata kuliah yang sedang berlangsung sesuai jadwal hari ini
        active_course = self.get_active_course_now()
        courses_to_scan = list(self.courses_cache)
        if active_course:
            k_act_id = active_course.get('nomor')
            courses_to_scan = [c for c in courses_to_scan if c.get('nomor') == k_act_id] + [c for c in courses_to_scan if c.get('nomor') != k_act_id]

        for c in courses_to_scan:
            k_id = c.get('nomor')
            schema = c.get('jenis_schema') or c.get('jenisSchema') or 0
            mk_obj = c.get('nama_matakuliah') or c.get('matakuliah')
            mk_nama = mk_obj.get('nama') if isinstance(mk_obj, dict) else (mk_obj or f"Kuliah #{k_id}")
            dosen = c.get('dosen') or "Dosen Pengampu"
            kuliah_asal = c.get('kuliah_asal') or k_id

            try:
                pres_resp = self.session.get(
                    'https://ethol.pens.ac.id/api/presensi/aktif-kuliah',
                    params={'kuliah': k_id, 'jenis_schema': schema},
                    timeout=8
                )

                # Jika sesi kedaluwarsa di tengah loop, re-login SSO instan dan ulangi 1x
                if pres_resp.status_code == 401:
                    if self.login_cas(notify_on_fail=False):
                        pres_resp = self.session.get(
                            'https://ethol.pens.ac.id/api/presensi/aktif-kuliah',
                            params={'kuliah': k_id, 'jenis_schema': schema},
                            timeout=8
                        )
                    else:
                        continue

                if pres_resp.status_code == 200:
                    pres_data = pres_resp.json()
                    key = self.extract_active_key(pres_data)

                    if key:
                        found_open += 1
                        unique_today_key = f"{today_str}_{key}"
                        if key in self.attended_keys or unique_today_key in self.attended_keys:
                            results.append(f"ℹ️ <b>{mk_nama}</b>: Presensi terbuka & sudah tercatat.")
                            continue

                        logger.info(f"⚡ Presensi Terbuka Ditemukan: {mk_nama} (Key: {key})")
                        payload = {
                            "kuliah": k_id,
                            "jenis_schema": schema,
                            "mahasiswa": self.user_info.get('nomor') if self.user_info else None,
                            "key": key,
                            "kuliah_asal": kuliah_asal
                        }
                        submit_resp = self.session.post('https://ethol.pens.ac.id/api/presensi/mahasiswa', json=payload, timeout=10)

                        # Retry submit jika sesi sempat timeout
                        if submit_resp.status_code == 401:
                            if self.login_cas(notify_on_fail=False):
                                payload["mahasiswa"] = self.user_info.get('nomor') if self.user_info else None
                                submit_resp = self.session.post('https://ethol.pens.ac.id/api/presensi/mahasiswa', json=payload, timeout=10)

                        if submit_resp.status_code == 200:
                            res_json = submit_resp.json()
                            pesan = res_json.get('pesan') or res_json.get('message') or 'Berhasil'
                            is_success = (
                                res_json.get('sukses') or
                                res_json.get('success') or
                                res_json.get('status') in [200, "200", True] or
                                "sudah" in str(pesan).lower() or
                                "berhasil" in str(pesan).lower()
                            )

                            if is_success:
                                success_msg = (
                                    "🎉 <b>PRESENSI BERHASIL DICATAT!</b>\n\n"
                                    f"📚 <b>Mata Kuliah:</b> {mk_nama}\n"
                                    f"👨‍🏫 <b>Dosen:</b> {dosen}\n"
                                    f"🔑 <b>Key:</b> <code>{key}</code>\n"
                                    f"🕒 <b>Waktu:</b> {now_str}\n"
                                    f"💬 <b>Respon:</b> {pesan}"
                                )
                                logger.info(f"Berhasil hadir: {mk_nama} - {pesan}")
                                self.send_tg(success_msg)
                                send_os_notification("Presensi Berhasil!", f"{mk_nama} berhasil diabsenkan ({pesan})")
                                self.attended_keys.add(key)
                                self.attended_keys.add(unique_today_key)
                                self.save_attended_state()
                                results.append(f"✅ <b>{mk_nama}</b>: {pesan}")
                            else:
                                msg = f"⚠️ [Status Presensi] {mk_nama}: {pesan}"
                                logger.warning(msg)
                                self.send_tg(msg)
                                results.append(msg)
                        else:
                            err = f"❌ Gagal kirim presensi {mk_nama} (HTTP {submit_resp.status_code})"
                            logger.error(err)
                            results.append(err)
            except Exception as e:
                logger.error(f"Error parse presensi {mk_nama}: {e}")

        if manual:
            if found_open == 0:
                return f"✅ <b>Pemindaian Selesai ({now_str})</b>\n\nTidak ada presensi yang sedang dibuka dosen pada {len(self.courses_cache)} mata kuliah Anda."
            return "\n".join(results)
        return "Scan otomatis selesai."

    def check_notifications_trigger(self):
        try:
            r = self.session.get('https://ethol.pens.ac.id/api/notifikasi/mahasiswa-belum-baca', timeout=8)
            if r.status_code == 401:
                self.ensure_valid_session()
                return

            if r.status_code == 200:
                count_data = r.json()
                if count_data.get('jumlah', 0) > 0:
                    notifs = self.session.get('https://ethol.pens.ac.id/api/notifikasi/mahasiswa', params={'filterNotif': 'SEMUA'}, timeout=8).json()
                    if isinstance(notifs, list):
                        for n in notifs[:5]:
                            n_id = n.get('idNotifikasi')
                            if n_id and n_id not in self.processed_notif_ids and str(n.get('status')) == "1":
                                self.processed_notif_ids.add(n_id)
                                kode = n.get('kodeNotifikasi')
                                ket = n.get('keterangan', '')

                                if kode == "PRESENSI-KULIAH":
                                    logger.info(f"🔔 Notifikasi Presensi ETHOL: {ket}")
                                    self.send_tg(f"🔔 <b>NOTIFIKASI ETHOL:</b>\n{ket}\n\n<i>Memicu auto-presensi seketika...</i>")
                                    send_os_notification("Notifikasi Presensi ETHOL", ket)
                                    self.scan_and_attend(manual=False)
                                elif kode == "TUGAS-BARU":
                                    self.send_tg(f"📝 <b>NOTIFIKASI TUGAS BARU:</b>\n{ket}")
                                    send_os_notification("Tugas Baru ETHOL", ket)

                                self.session.put('https://ethol.pens.ac.id/api/notifikasi/mahasiswa-baca-notif', json={'idNotifikasi': n_id}, timeout=5)
        except Exception as e:
            logger.warning(f"Error checking notifications: {e}")

    def get_attendance_statistics(self):
        self.update_cache()
        nomor_mhs = self.user_info.get('nomor') if self.user_info else None
        if not nomor_mhs:
            return None

        today_date_str = get_wib_now().strftime("%d-%m-%Y")
        total_dosen_semester = 0
        total_mhs_semester = 0
        total_dosen_today = 0
        total_mhs_today = 0
        course_breakdown = []

        for c in self.courses_cache:
            k_id = c.get('nomor')
            schema = c.get('jenis_schema') or c.get('jenisSchema') or 0
            mk = c.get('nama_matakuliah') or c.get('matakuliah')
            mk_name = mk.get('nama') if isinstance(mk, dict) else mk
            dosen_nomor = c.get('nomor_dosen')

            try:
                mhs_resp = self.session.get(
                    'https://ethol.pens.ac.id/api/presensi/riwayat',
                    params={'kuliah': k_id, 'jenis_schema': schema, 'nomor': nomor_mhs},
                    timeout=5
                )
                mhs_list = mhs_resp.json() if mhs_resp.status_code == 200 and isinstance(mhs_resp.json(), list) else []

                dosen_resp = self.session.get(
                    'https://ethol.pens.ac.id/api/presensi/get-tanggal-presensi-dosen-per-semester',
                    params={'tahun': self.tahun_aktif, 'semester': self.semester_aktif, 'kuliah': k_id, 'dosen': dosen_nomor},
                    timeout=5
                )
                dosen_list = dosen_resp.json() if dosen_resp.status_code == 200 and isinstance(dosen_resp.json(), list) else []

                d_today = sum(1 for d in dosen_list if today_date_str in str(d.get('waktu_indonesia', '')) or today_date_str in str(d.get('waktu', '')))
                m_today = sum(1 for m in mhs_list if today_date_str in str(m.get('tanggal', '')) or today_date_str in str(m.get('waktu_indonesia', '')))

                total_dosen_semester += len(dosen_list)
                total_mhs_semester += len(mhs_list)
                total_dosen_today += d_today
                total_mhs_today += m_today

                course_breakdown.append({
                    "kuliah_id": k_id,
                    "nama": mk_name,
                    "hadir": len(mhs_list),
                    "total": len(dosen_list),
                    "d_today": d_today,
                    "m_today": m_today
                })
            except Exception:
                pass

        pct = 100.0 if total_dosen_semester == 0 else (total_mhs_semester / total_dosen_semester) * 100.0
        return {
            "percentage": pct,
            "total_dosen_semester": total_dosen_semester,
            "total_mhs_semester": total_mhs_semester,
            "total_dosen_today": total_dosen_today,
            "total_mhs_today": total_mhs_today,
            "breakdown": course_breakdown
        }

    def get_pending_tasks(self):
        self.update_cache()
        pending = []
        for c in self.courses_cache:
            k_id = c.get('nomor')
            schema = c.get('jenis_schema') or c.get('jenisSchema') or 0
            mk = c.get('nama_matakuliah') or c.get('matakuliah')
            mk_name = mk.get('nama') if isinstance(mk, dict) else mk

            try:
                res = self.session.get('https://ethol.pens.ac.id/api/tugas', params={'kuliah': k_id, 'jenisSchema': schema}, timeout=5)
                if res.status_code == 200:
                    tasks = res.json()
                    if isinstance(tasks, list):
                        for t in tasks:
                            if not t.get('submission_time') and str(t.get('tutup', '0')) != "1":
                                pending.append({
                                    "kuliah_id": k_id,
                                    "matkul": mk_name,
                                    "title": t.get('title') or t.get('judul'),
                                    "deadline": t.get('deadline_indonesia') or t.get('deadline') or "-"
                                })
            except Exception:
                pass
        return pending

    def format_status_text(self, student_acc=None, is_admin=False):
        # Jika dipanggil untuk akun spesifik (member), tampilkan status personal
        if student_acc and not is_admin:
            return student_acc.format_status_text(self)

        # Untuk Admin: Jika ada 2+ akun, sajikan Master Operational Dashboard
        if is_admin and len(self.accounts) > 1:
            now_str = get_wib_str()
            txt = (
                "👑 <b>DASHBOARD MONITORING MULTI-MAHASISWA</b>\n"
                f"<i>Total Terdaftar: {len(self.accounts)} Akun Mahasiswa Aktif</i>\n\n"
            )
            for idx, acc in enumerate(self.accounts, 1):
                nrp = acc.user_info.get('nipnrp', 'N/A') if acc.user_info else '-'
                nama = acc.user_info.get('nama', acc.name) if acc.user_info else acc.name
                sso_stat = "🟢 Terhubung" if acc.user_info else "🔴 Terputus (/relogin)"
                tg_stat = f"🟢 <code>{acc.telegram_chat_id}</code>" if acc.telegram_chat_id else "⚪ Belum Ditautkan"
                tag = " (Admin)" if idx == 1 else ""

                txt += (
                    f"<b>[{idx}] {nama}</b>{tag}\n"
                    f"    • NRP      : <code>{nrp}</code>\n"
                    f"    • Sesi SSO : {sso_stat}\n"
                    f"    • Telegram : {tg_stat}\n"
                    f"    • Auth     : <code>{acc.last_auth_time}</code>\n\n"
                )
            txt += (
                "<b>⚙️ STATUS SCANNER SISTEM:</b>\n"
                f"• Mode Operasional : <b>{self.determine_mode()}</b>\n"
                f"• Terakhir Scan    : <code>{self.last_scan_time}</code>\n"
                f"• Waktu Server     : <code>{now_str}</code>"
            )
            return txt

        # Fallback single account
        if self.accounts:
            return self.accounts[0].format_status_text(self)
        return "Belum ada akun mahasiswa aktif."

    def format_rekap_detail(self, student_acc=None, is_admin=False, target_arg=None):
        if student_acc and not is_admin:
            return student_acc.format_rekap_detail()

        # Admin: cek jika meminta mahasiswa spesifik (contoh: /rekap 2)
        if is_admin and target_arg and str(target_arg).isdigit():
            idx = int(target_arg) - 1
            if 0 <= idx < len(self.accounts):
                return self.accounts[idx].format_rekap_detail()

        # Admin Multi-Akun (≥ 2): Sajikan Rekapitulasi Agregat Komparatif
        if is_admin and len(self.accounts) > 1:
            txt = (
                "📊 <b>REKAPITULASI KEHADIRAN MULTI-MAHASISWA</b>\n"
                f"<i>Tinjauan Presensi Semester ({len(self.accounts)} Mahasiswa Terdaftar)</i>\n\n"
                "🏆 <b>RINGKASAN TINGKAT KEHADIRAN:</b>\n"
            )
            all_stats = []
            for idx, acc in enumerate(self.accounts, 1):
                stats = acc.get_attendance_statistics()
                all_stats.append((acc, stats))
                m_name = acc.user_info.get('nama', acc.name) if acc.user_info else acc.name
                if stats:
                    pct = stats['percentage']
                    hadir = stats['total_mhs_semester']
                    total = stats['total_dosen_semester']
                    h_today = stats['total_mhs_today']
                    badge = "🟢" if pct >= 80 else "🟠" if pct >= 60 else "🔴"
                    txt += f"{badge} <b>{idx}. {m_name}</b> : <b>{pct:.1f}%</b> ({hadir}/{total} sesi) • Hari ini: {h_today}\n"
                else:
                    txt += f"⚪ <b>{idx}. {m_name}</b> : <i>Belum sinkron</i>\n"

            txt += "\n" + ("═" * 32) + "\n\n"
            txt += "📌 <b>STATUS PRESENSI HARI INI:</b>\n\n"
            for idx, (acc, stats) in enumerate(all_stats, 1):
                m_name = acc.user_info.get('nama', acc.name) if acc.user_info else acc.name
                txt += f"<b>[{idx}] {m_name}:</b>\n"
                if not stats:
                    txt += "  <i>Gagal memuat rincian kuliah.</i>\n\n"
                    continue
                found_any = False
                for item in stats['breakdown']:
                    if item.get('m_today', 0) > 0 or item.get('d_today', 0) > 0:
                        found_any = True
                        m_today = item['m_today']
                        d_today = item['d_today']
                        tag = "🟢 Hadir" if m_today > 0 else "⚠️ Belum Hadir"
                        txt += f"  • {item['nama']}: {tag} ({m_today}/{d_today} sesi)\n"
                if not found_any:
                    txt += "  • <i>Tidak ada perkuliahan aktif hari ini.</i>\n"
                txt += f"  • Total Kehadiran: {stats['total_mhs_semester']} kali hadir semester ini.\n\n"

            txt += "💡 <i>Ketik <code>/rekap 1</code> atau <code>/rekap 2</code> untuk melihat detail per mahasiswa.</i>"
            return txt

        if self.accounts:
            return self.accounts[0].format_rekap_detail()
        return "Belum ada akun mahasiswa aktif."

    def format_tugas_text(self, student_acc=None, is_admin=False, target_arg=None):
        if student_acc and not is_admin:
            return student_acc.format_tugas_text()

        # Admin: cek jika meminta mahasiswa spesifik (/tugas 2)
        if is_admin and target_arg and str(target_arg).isdigit():
            idx = int(target_arg) - 1
            if 0 <= idx < len(self.accounts):
                return self.accounts[idx].format_tugas_text()

        # Admin Multi-Akun (≥ 2): Sajikan Rekapitulasi Tugas Agregat
        if is_admin and len(self.accounts) > 1:
            txt = (
                "📝 <b>REKAPITULASI TUGAS MULTI-MAHASISWA</b>\n"
                f"<i>Pemantauan tugas aktif untuk {len(self.accounts)} mahasiswa</i>\n\n"
            )
            for idx, acc in enumerate(self.accounts, 1):
                m_name = acc.user_info.get('nama', acc.name) if acc.user_info else acc.name
                tasks = acc.get_pending_tasks()
                txt += f"👤 <b>[{idx}] {m_name}</b> — <b>{len(tasks)} Tugas Pending:</b>\n"
                if not tasks:
                    txt += "   🎉 <i>Semua tugas tuntas dikerjakan!</i>\n\n"
                else:
                    for t_idx, t in enumerate(tasks, 1):
                        mk = t.get('matkul', 'Mata Kuliah')
                        title = t.get('title') or t.get('judul')
                        deadline = t.get('deadline') or '-'
                        txt += f"   {t_idx}. <b>{title}</b>\n      📚 {mk} • ⏰ <code>{deadline}</code>\n"
                    txt += "\n"

            txt += "💡 <i>Ketik <code>/tugas 1</code> atau <code>/tugas 2</code> untuk tautan web pengumpulan per mahasiswa.</i>"
            return txt

        if self.accounts:
            return self.accounts[0].format_tugas_text()
        return "Belum ada akun mahasiswa aktif."

    def format_jadwal_text(self, student_acc=None, is_admin=False, target_arg=None):
        if student_acc and not is_admin:
            return student_acc.format_jadwal_text()

        # Admin: cek jika meminta mahasiswa spesifik (/jadwal 2)
        if is_admin and target_arg and str(target_arg).isdigit():
            idx = int(target_arg) - 1
            if 0 <= idx < len(self.accounts):
                return self.accounts[idx].format_jadwal_text()

        # Admin: cek jika meminta jadwal lengkap semua hari (/jadwal all)
        if is_admin and target_arg and target_arg.lower() == 'all' and len(self.accounts) > 1:
            txt = "🗓️ <b>JADWAL LENGKAP MULTI-MAHASISWA (SEMUA HARI):</b>\n\n"
            for idx, acc in enumerate(self.accounts, 1):
                m_name = acc.user_info.get('nama', acc.name) if acc.user_info else acc.name
                txt += f"👤 <b>[{idx}] {m_name.upper()}:</b>\n"
                txt += acc.format_jadwal_text() + "\n" + ("═" * 30) + "\n\n"
            return txt

        # Admin Multi-Akun (≥ 2): Sajikan Agenda Kuliah Hari Ini Per Mahasiswa
        if is_admin and len(self.accounts) > 1:
            now_wib = get_wib_now()
            today_idx = now_wib.weekday()
            day_names = {0: "senin", 1: "selasa", 2: "rabu", 3: "kamis", 4: "jumat", 5: "sabtu", 6: "minggu"}
            today_day = day_names.get(today_idx, "")

            txt = (
                f"🗓️ <b>AGENDA KULIAH HARI INI ({today_day.upper()})</b>\n"
                f"<i>Pemantauan jadwal perkuliahan untuk {len(self.accounts)} mahasiswa</i>\n\n"
            )
            for idx, acc in enumerate(self.accounts, 1):
                m_name = acc.user_info.get('nama', acc.name) if acc.user_info else acc.name
                acc.update_cache()
                today_items = [it for it in acc.schedule_cache if str(it.get('hari', '')).lower().replace("'", "").strip() == today_day]

                txt += f"👤 <b>[{idx}] {m_name.upper()}:</b>\n"
                if today_items:
                    for it in today_items:
                        jam = f"{it.get('jam_awal', '-')} - {it.get('jam_akhir', '-')}"
                        mk = it.get('matakuliah', '-')
                        ruang = it.get('ruang') or 'Online'
                        txt += f"  • <code>{jam} WIB</code>: <b>{mk}</b> (📍 {ruang})\n"
                else:
                    txt += "  • <i>Tidak ada perkuliahan terjadwal hari ini.</i>\n"
                txt += "\n"

            txt += (
                "💡 <b>Pilihan Jadwal Lengkap:</b>\n"
                "• Detail seminggu per mahasiswa: <code>/jadwal 1</code> atau <code>/jadwal 2</code>\n"
                "• Detail seminggu semua mahasiswa: <code>/jadwal all</code>"
            )
            return txt

        if self.accounts:
            return self.accounts[0].format_jadwal_text()
        return "Belum ada akun mahasiswa aktif."

    def get_raw_logs(self, max_lines=15):
        if not os.path.exists(LOG_FILE):
            return "File log belum tersedia."
        try:
            with open(LOG_FILE, 'r', encoding='utf-8', errors='ignore') as f:
                lines = [l.strip() for l in f.readlines() if l.strip()]
            if not lines:
                return "File log masih kosong."
            selected = lines[-max_lines:]
            escaped = [html.escape(l) for l in selected]
            return f"📜 <b>LOG AKTIVITAS TERBARU (WIB):</b>\n\n<code>" + "\n".join(escaped) + "</code>"
        except Exception as e:
            return f"Gagal membaca file log: {e}"

    def handle_tg_command(self, cmd, user_msg_id=None, chat_id=None):
        target_chat_id = str(chat_id or self.tg_chat_id).strip()
        c = cmd.lower().strip()
        logger.info(f"Menerima perintah Telegram dari [{target_chat_id}]: {cmd}")
        credit = "\n\n✦ <b>Creator : Gungna</b>"

        is_admin = self.is_admin(target_chat_id)
        current_acc = self.get_account_by_chat_id(target_chat_id)

        if target_chat_id not in self.chat_last_interaction:
            self.chat_last_interaction[target_chat_id] = []

        # Kirim loading bar di chat pemanggil
        loading_id = self.start_loading_bar("Memproses perintah...", chat_id=target_chat_id)

        # Hapus pesan interaksi sebelumnya khusus chat ini
        self.delete_tg_messages(self.chat_last_interaction[target_chat_id], chat_id=target_chat_id)
        self.chat_last_interaction[target_chat_id] = []

        current_batch = []
        if user_msg_id:
            current_batch.append(user_msg_id)

        # Skenario 1: User belum terhubung / belum punya akun terikat di KON-THOL
        if not current_acc and not is_admin:
            if cmd.startswith('/bind'):
                parts = cmd[len('/bind'):].strip().split()
                if len(parts) < 2:
                    res_id = self.send_tg(
                        "⚠️ <b>Format Perintah /bind:</b>\n"
                        "<code>/bind email@student.pens.ac.id password</code>\n\n"
                        "<i>Contoh:</i>\n"
                        "<code>/bind nazriel@student.pens.ac.id Rahasia123</code>"
                        + credit,
                        chat_id=target_chat_id
                    )
                    if res_id: current_batch.append(res_id)
                else:
                    email_in = parts[0]
                    pass_in = parts[1]
                    if loading_id:
                        self.advance_loading_bar(loading_id, "Memvalidasi kredensial SSO PENS...", chat_id=target_chat_id)
                    succ, acc_res = self.bind_telegram_account(target_chat_id, email_in, pass_in)
                    if succ:
                        m_nama = acc_res.user_info.get('nama', acc_res.name) if acc_res.user_info else acc_res.name
                        nrp = acc_res.user_info.get('nipnrp', '') if acc_res.user_info else ''
                        res_id = self.send_tg(
                            "🎉 <b>AKUN TELEGRAM BERHASIL DITAUTKAN!</b>\n\n"
                            f"👤 <b>Mahasiswa:</b> {m_nama}\n"
                            f"🆔 <b>NRP:</b> <code>{nrp}</code>\n"
                            f"💬 <b>Telegram ID:</b> <code>{target_chat_id}</code>\n\n"
                            "Sekarang Anda dapat menggunakan bot ini secara mandiri!\n"
                            "Ketik /menu untuk membuka panduan layanan."
                            + credit,
                            chat_id=target_chat_id
                        )
                    else:
                        res_id = self.send_tg(
                            "❌ <b>GAGAL MENAUTKAN AKUN</b>\n\n"
                            f"Keterangan: {acc_res}\n\n"
                            "Pastikan email dan password SSO PENS Anda sudah benar."
                            + credit,
                            chat_id=target_chat_id
                        )
                    if res_id: current_batch.append(res_id)

                self.chat_last_interaction[target_chat_id] = current_batch
                if loading_id:
                    self.finish_loading_bar(loading_id, chat_id=target_chat_id)
                return

            else:
                # User asing / belum tertaut mengirim perintah lain atau /start
                res_id = self.send_tg(
                    "👋 <b>Halo! Selamat datang di KON-THOL Assistant.</b>\n\n"
                    f"Akun Telegram Anda (ID: <code>{target_chat_id}</code>) belum ditautkan ke akun mahasiswa ETHOL manapun.\n\n"
                    "<b>Cara Menautkan Akun Anda:</b>\n"
                    "Ketik perintah:\n"
                    "<code>/bind email@student.pens.ac.id password</code>\n\n"
                    "<i>Setelah ditautkan, Anda dapat mengecek jadwal, rekap presensi, tugas, dan melakukan presensi mandiri dari Telegram Anda!</i>"
                    + credit,
                    chat_id=target_chat_id
                )
                if res_id: current_batch.append(res_id)
                self.chat_last_interaction[target_chat_id] = current_batch
                if loading_id:
                    self.finish_loading_bar(loading_id, chat_id=target_chat_id)
                return

        # Skenario 2: User terdaftar (atau Admin)
        if c in ['/start', '/help', 'help', '/menu']:
            old_menu_id = self.chat_main_menu.get(target_chat_id)
            if old_menu_id:
                self.delete_tg_message(old_menu_id, chat_id=target_chat_id)
                self.chat_main_menu[target_chat_id] = None
            if user_msg_id:
                self.delete_tg_message(user_msg_id, chat_id=target_chat_id)

            if loading_id:
                self.advance_loading_bar(loading_id, "Menyiapkan menu...", chat_id=target_chat_id)

            menu_text = self.format_menu_text(student_acc=current_acc, is_admin=is_admin)
            new_menu_id = self.send_tg_photo(menu_text, chat_id=target_chat_id)
            self.chat_main_menu[target_chat_id] = new_menu_id

            if loading_id:
                self.finish_loading_bar(loading_id, chat_id=target_chat_id)
            return

        if loading_id:
            self.advance_loading_bar(loading_id, "Mengambil data...", chat_id=target_chat_id)

        # Parsing argumen tambahan jika ada (contoh: /jadwal 2, /rekap all, dll)
        cmd_parts = cmd.split(maxsplit=1)
        sub_arg = cmd_parts[1].strip() if len(cmd_parts) > 1 else None

        # Perintah Mahasiswa / Dashboard:
        if c.startswith('/jadwal') or c.startswith('/matkul') or c.startswith('jadwal'):
            jadwal_txt = self.format_jadwal_text(student_acc=current_acc, is_admin=is_admin, target_arg=sub_arg)
            res_id = self.send_tg(f"{jadwal_txt}{credit}", chat_id=target_chat_id)
            if res_id: current_batch.append(res_id)

        elif c.startswith('/rekap') or c.startswith('rekap'):
            rekap_txt = self.format_rekap_detail(student_acc=current_acc, is_admin=is_admin, target_arg=sub_arg)
            res_id = self.send_tg(f"{rekap_txt}{credit}", chat_id=target_chat_id)
            if res_id: current_batch.append(res_id)

        elif c.startswith('/tugas') or c.startswith('tugas'):
            tugas_txt = self.format_tugas_text(student_acc=current_acc, is_admin=is_admin, target_arg=sub_arg)
            res_id = self.send_tg(f"{tugas_txt}{credit}", chat_id=target_chat_id)
            if res_id: current_batch.append(res_id)

        elif c.startswith('/status') or c.startswith('status'):
            status_txt = self.format_status_text(student_acc=current_acc, is_admin=is_admin)
            res_id = self.send_tg(f"{status_txt}{credit}", chat_id=target_chat_id)
            if res_id: current_batch.append(res_id)

        elif c.startswith('/scan') or c.startswith('/absen') or c.startswith('scan') or c.startswith('absen'):
            if is_admin and sub_arg and sub_arg.isdigit():
                idx = int(sub_arg) - 1
                if 0 <= idx < len(self.accounts):
                    res = self.accounts[idx].scan_and_attend(notify_callback=self.notify_attendance, manual=True)
                else:
                    res = f"❌ Akun urutan #{sub_arg} tidak ditemukan."
            elif is_admin and len(self.accounts) > 1:
                res = self.scan_and_attend(manual=True)
            elif current_acc:
                res = current_acc.scan_and_attend(notify_callback=self.notify_attendance, manual=True)
            else:
                res = self.scan_and_attend(manual=True)
            res_id = self.send_tg(f"{res}{credit}", chat_id=target_chat_id)
            if res_id: current_batch.append(res_id)

        elif c in ['/relogin', 'relogin']:
            target_obj = current_acc if current_acc else self
            if target_obj.login_cas(notify_on_fail=False):
                res_id = self.send_tg(f"✅ Berhasil login ulang ke SSO PENS untuk {target_obj.name}!{credit}", chat_id=target_chat_id)
            else:
                res_id = self.send_tg(f"❌ Gagal login ulang ke SSO PENS untuk {target_obj.name}.{credit}", chat_id=target_chat_id)
            if res_id: current_batch.append(res_id)

        # Perintah Admin:
        elif cmd.startswith('/addaccount') or cmd.startswith('/addakun'):
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Perintah /addaccount dibatasi hanya untuk Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                raw_args = cmd[len(cmd.split()[0]):].strip()
                parts = [p.strip() for p in raw_args.split('|') if p.strip()] if '|' in raw_args else raw_args.split()
                if len(parts) < 3:
                    res_id = self.send_tg(
                        "⚠️ <b>Format Perintah /addaccount:</b>\n"
                        "<code>/addaccount Nama Mahasiswa | email@student.pens.ac.id | password [| telegram_chat_id]</code>\n\n"
                        "<i>Contoh:</i>\n"
                        "<code>/addaccount Nazriel | nazriel@student.pens.ac.id | Rahasia123 | 1234567890</code>"
                        + credit,
                        chat_id=target_chat_id
                    )
                    if res_id: current_batch.append(res_id)
                else:
                    name = parts[0]
                    user_acc = parts[1]
                    pass_acc = parts[2]
                    tg_id = parts[3] if len(parts) > 3 else ""
                    if loading_id:
                        self.advance_loading_bar(loading_id, "Memvalidasi kredensial ke SSO PENS...", chat_id=target_chat_id)
                    succ, info = self.add_account(name, user_acc, pass_acc, tg_id=tg_id)
                    if succ:
                        res_id = self.send_tg(
                            f"✅ <b>AKUN BERHASIL DITAMBAHKAN</b>\n\n"
                            f"{info}\n"
                            f"Telegram ID: <code>{tg_id or 'Belum diatur (bisa pakai /settelegram atau /bind)'}</code>\n"
                            f"Akun ini sekarang otomatis dipantau oleh KON-THOL Multi-Account!"
                            + credit,
                            chat_id=target_chat_id
                        )
                    else:
                        res_id = self.send_tg(f"❌ <b>GAGAL MENAMBAHKAN AKUN</b>\n\n{info}{credit}", chat_id=target_chat_id)
                    if res_id: current_batch.append(res_id)

        elif cmd.startswith('/settelegram') or cmd.startswith('/settg'):
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Perintah ini dibatasi hanya untuk Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                parts = cmd.split(maxsplit=2)
                if len(parts) < 3:
                    res_id = self.send_tg(
                        "⚠️ <b>Format Perintah /settelegram:</b>\n"
                        "<code>/settelegram email_atau_urutan telegram_chat_id</code>\n\n"
                        "<i>Contoh:</i>\n"
                        "<code>/settelegram 2 1234567890</code>"
                        + credit,
                        chat_id=target_chat_id
                    )
                    if res_id: current_batch.append(res_id)
                else:
                    target_val = parts[1].strip()
                    tg_id_val = parts[2].strip()
                    succ, msg = self.set_telegram_id(target_val, tg_id_val)
                    icon = "✅" if succ else "❌"
                    res_id = self.send_tg(f"{icon} {msg}{credit}", chat_id=target_chat_id)
                    if res_id: current_batch.append(res_id)

        elif cmd.startswith('/delaccount') or cmd.startswith('/delakun'):
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Perintah /delaccount dibatasi hanya untuk Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                parts = cmd.split(maxsplit=1)
                if len(parts) < 2:
                    res_id = self.send_tg(
                        "⚠️ <b>Format Perintah /delaccount:</b>\n"
                        "<code>/delaccount email@student.pens.ac.id</code> atau urutan akun (contoh: <code>/delaccount 2</code>)"
                        + credit,
                        chat_id=target_chat_id
                    )
                    if res_id: current_batch.append(res_id)
                else:
                    target = parts[1].strip()
                    succ, msg = self.del_account(target)
                    icon = "✅" if succ else "❌"
                    res_id = self.send_tg(f"{icon} {msg}{credit}", chat_id=target_chat_id)
                    if res_id: current_batch.append(res_id)

        elif c in ['/accounts', '/multi', '/daftarakun', 'accounts', 'multi']:
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Daftar akun lengkap hanya dapat dilihat oleh Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                res_id = self.send_tg(f"{self.format_accounts_list()}{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)

        elif c in ['/scanall', 'scanall']:
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Scan seluruh akun serentak hanya dapat dipicu oleh Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                res = self.scan_and_attend(manual=True)
                res_id = self.send_tg(f"{res}{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)

        elif c in ['/cooldown', 'cooldown']:
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Kontrol scanner dibatasi hanya untuk Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                _, msg = self.activate_cooldown()
                res_id = self.send_tg(f"💤 {msg}{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)

        elif c in ['/resume', '/siaga', 'resume', 'siaga']:
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Kontrol scanner dibatasi hanya untuk Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                _, msg = self.deactivate_cooldown()
                res_id = self.send_tg(f"⚡ {msg}{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)

        elif c in ['/log', '/logs', 'log']:
            if not is_admin:
                res_id = self.send_tg(f"🔒 <i>Akses log sistem dibatasi hanya untuk Administrator.</i>{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)
            else:
                res_id = self.send_tg(f"{self.get_raw_logs(15)}{credit}", chat_id=target_chat_id)
                if res_id: current_batch.append(res_id)

        else:
            res_id = self.send_tg(f"Perintah tidak dikenal: <code>{html.escape(cmd)}</code>. Ketik /help untuk panduan.{credit}", chat_id=target_chat_id)
            if res_id: current_batch.append(res_id)

        self.chat_last_interaction[target_chat_id] = current_batch
        if loading_id:
            self.finish_loading_bar(loading_id, chat_id=target_chat_id)

    def run_auto_loop(self, interval=120):
        logger.info(f"Scanner background aktif (interval {interval}s)...")
        last_scan_tick = 0
        while True:
            try:
                now_wib = get_wib_now()
                time_val = now_wib.hour + now_wib.minute / 60.0

                # 1. Cek pergantian mode operasional & update Telegram UI otomatis
                mode_now = self.determine_mode(now_wib)
                if mode_now != self.current_mode:
                    old_mode = self.current_mode
                    self.current_mode = mode_now
                    if old_mode is not None:
                        logger.info(f"Pergantian mode operasional: {old_mode} -> {mode_now}")
                        self.notify_mode_change(old_mode, mode_now)
                        self.update_telegram_menu_ui()

                # 2. Atur interval scan berdasarkan mode
                if self.is_cooldown_active_today():
                    time.sleep(30)
                    continue

                if not self.force_siaga and (time_val >= 21.5 or time_val < 4.0):
                    # Istirahat Malam: scan tiap 300s
                    if time.time() - last_scan_tick > 300:
                        self.check_notifications_trigger()
                        self.scan_and_attend(manual=False)
                        last_scan_tick = time.time()
                    time.sleep(30)
                    continue

                if 4.0 <= time_val < 6.5:
                    # Siaga Subuh: scan tiap 180s
                    if time.time() - last_scan_tick > 180:
                        self.check_notifications_trigger()
                        self.scan_and_attend(manual=False)
                        last_scan_tick = time.time()
                    time.sleep(20)
                    continue

                # Siaga Normal: scan adaptif 30 detik s/d 60 detik (hemat daya & human-like)
                import random
                if not hasattr(self, '_current_normal_interval') or not self._current_normal_interval:
                    self._current_normal_interval = random.randint(30, 60)

                if time.time() - last_scan_tick > self._current_normal_interval:
                    self.check_notifications_trigger()
                    self.scan_and_attend(manual=False)
                    last_scan_tick = time.time()
                    self._current_normal_interval = random.randint(30, 60)
                time.sleep(5)
            except Exception as e:
                logger.error(f"Error pada auto loop: {e}")
                time.sleep(15)

    def run_tg_listener(self):
        if not self.tg_token:
            return
        logger.info("Telegram Bot Listener aktif (Multi-User Mandiri)...")
        offset = 0
        while True:
            try:
                url = f"https://api.telegram.org/bot{self.tg_token}/getUpdates?offset={offset}&timeout=20"
                resp = requests.get(url, timeout=25)
                if resp.status_code == 200:
                    data = resp.json()
                    for update in data.get('result', []):
                        offset = update['update_id'] + 1
                        msg = update.get('message', {})
                        chat_id = str(msg.get('chat', {}).get('id', ''))
                        text = msg.get('text', '').strip()
                        msg_id = msg.get('message_id')

                        if not chat_id:
                            continue

                        if text.startswith('/'):
                            self.handle_tg_command(text, user_msg_id=msg_id, chat_id=chat_id)
            except Exception:
                time.sleep(3)

    def execute_cli(self, command):
        """Eksekusi perintah dari Terminal / CLI (mencetak teks bersih tanpa tag HTML)."""
        cmd = command.lower().strip()
        credit = "\n\n✦ Creator : Gungna"
        if cmd in ['accounts', 'multi']:
            print(to_plain_text(self.format_accounts_list()) + credit)
        elif cmd in ['scanall']:
            print("[*] Sedang memindai presensi seluruh akun terdaftar...")
            print(to_plain_text(self.scan_and_attend(manual=True)) + credit)
        elif cmd in ['scan', 'absen']:
            print("[*] Sedang memindai presensi di server ETHOL...")
            print(to_plain_text(self.scan_and_attend(manual=True)) + credit)
        elif cmd in ['jadwal', 'matkul']:
            print(to_plain_text(self.format_jadwal_text()) + credit)
        elif cmd in ['rekap']:
            print(to_plain_text(self.format_rekap_detail()) + credit)
        elif cmd in ['tugas']:
            print(to_plain_text(self.format_tugas_text()) + credit)
        elif cmd in ['log', 'logs']:
            print(to_plain_text(self.get_raw_logs(15)) + credit)
        elif cmd in ['status']:
            print(to_plain_text(self.format_status_text()) + credit)
        elif cmd in ['cooldown']:
            _, msg = self.activate_cooldown()
            print(f"[+] {msg}" + credit)
        elif cmd in ['resume']:
            _, msg = self.deactivate_cooldown()
            print(f"[+] {msg}" + credit)
        elif cmd in ['relogin']:
            print("[*] Melakukan login ulang CAS SSO...")
            if self.login_cas():
                print("[+] Berhasil login ulang ke SSO PENS!" + credit)
            else:
                print("[-] Gagal login ulang SSO PENS." + credit)
        elif cmd in ['help']:
            print("Perintah tersedia: scan, jadwal, rekap, tugas, log, status, cooldown, resume, relogin, quit")
        else:
            print(f"Perintah tidak dikenal: '{cmd}'. Ketik 'help' untuk panduan.")

def interactive_loop(bot):
    print("\n========================================================")
    print("   KON-THOL CLI — Asisten Akademik & Auto Presensi      ")
    print("   Ketik 'help' untuk daftar perintah atau 'quit'       ")
    print("========================================================\n")
    while True:
        try:
            line = input("KON-THOL > ").strip()
            if not line:
                continue
            if line.lower() in ['exit', 'quit', 'q']:
                print("[*] Menghentikan bot. Sampai jumpa!")
                os._exit(0)
            bot.execute_cli(line)
        except (KeyboardInterrupt, EOFError):
            print("\n[*] Selesai.")
            break

def main():
    parser = argparse.ArgumentParser(description="KON-THOL: Otomasi Presensi & Pendamping Akademik ETHOL PENS")
    parser.add_argument("--scan", action="store_true", help="Pindai dan absenkan presensi sekarang")
    parser.add_argument("--jadwal", action="store_true", help="Tampilkan jadwal kuliah")
    parser.add_argument("--rekap", action="store_true", help="Tampilkan rekap kehadiran semester")
    parser.add_argument("--tugas", action="store_true", help="Tampilkan daftar tugas pending")
    parser.add_argument("--log", action="store_true", help="Tampilkan riwayat log aktivitas terbaru")
    parser.add_argument("--status", action="store_true", help="Tampilkan status akun dan scanner")
    parser.add_argument("--cooldown", action="store_true", help="Aktifkan mode cooldown hari ini")
    parser.add_argument("--resume", action="store_true", help="Batalkan mode cooldown")
    parser.add_argument("--relogin", action="store_true", help="Login ulang SSO PENS")
    parser.add_argument("--daemon", action="store_true", help="Jalankan scanner di background tanpa prompt CLI")
    args = parser.parse_args()

    bot = EtholBot()
    bot.login_cas()

    # Eksekusi argumen satu kali jika diberikan
    if args.scan:
        bot.execute_cli("scan")
        return
    if args.jadwal:
        bot.execute_cli("jadwal")
        return
    if args.rekap:
        bot.execute_cli("rekap")
        return
    if args.tugas:
        bot.execute_cli("tugas")
        return
    if args.log:
        bot.execute_cli("log")
        return
    if args.status:
        bot.execute_cli("status")
        return
    if args.cooldown:
        bot.execute_cli("cooldown")
        return
    if args.resume:
        bot.execute_cli("resume")
        return
    if args.relogin:
        bot.execute_cli("relogin")
        return

    # Jalankan scanner background thread
    t_scan = threading.Thread(target=bot.run_auto_loop, args=(120,), daemon=True)
    t_scan.start()

    # Jalankan Telegram Listener jika token diisi
    if bot.tg_token:
        t_tg = threading.Thread(target=bot.run_tg_listener, daemon=True)
        t_tg.start()

    if args.daemon:
        try:
            while True:
                time.sleep(3600)
        except KeyboardInterrupt:
            print("\n[*] Service dihentikan.")
    else:
        interactive_loop(bot)

if __name__ == '__main__':
    main()
