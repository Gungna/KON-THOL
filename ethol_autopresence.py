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
        os.path.join("/opt/ethol-autopresence", filename)
    ]:
        if os.path.exists(candidate):
            return candidate
    return os.path.join(BASE_DIR, filename)

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
            'Accept': 'application/json, text/plain, */*'
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
        self.main_menu_msg_id = None
        self.last_interaction_msg_ids = []
        self.force_siaga = False

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

    def is_cooldown_active_today(self):
        return self.cooldown_date == get_wib_now().strftime("%Y-%m-%d")

    def activate_cooldown(self):
        today = get_wib_now().strftime("%Y-%m-%d")
        self.force_siaga = False
        if self.cooldown_date == today:
            return False, f"Mode Cooldown sudah aktif untuk hari ini ({today})."
        self.cooldown_date = today
        self.save_attended_state()
        return True, f"Mode Cooldown aktif untuk hari ini ({today}). Polling agresif dijeda hingga esok hari."

    def deactivate_cooldown(self):
        self.cooldown_date = None
        self.force_siaga = True
        self.save_attended_state()
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

    def send_tg_photo(self, caption, photo_path=None):
        if not self.tg_token or not self.tg_chat_id:
            return None
        p = photo_path or self.get_banner_path()
        if p and os.path.exists(p) and len(caption) <= 1024:
            try:
                url = f"https://api.telegram.org/bot{self.tg_token}/sendPhoto"
                with open(p, 'rb') as photo:
                    r = requests.post(url, data={
                        "chat_id": self.tg_chat_id,
                        "caption": caption,
                        "parse_mode": "HTML"
                    }, files={"photo": photo}, timeout=12)
                    if r.status_code == 200:
                        data = r.json()
                        if data.get('ok'):
                            return data['result'].get('message_id')
            except Exception as e:
                logger.error(f"Gagal kirim banner Telegram: {e}")
        return self.send_tg(caption)

    def send_tg(self, text):
        if not self.tg_token or not self.tg_chat_id:
            return None
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/sendMessage"
            r = requests.post(url, json={
                "chat_id": self.tg_chat_id,
                "text": text,
                "parse_mode": "HTML"
            }, timeout=8)
            if r.status_code == 200:
                data = r.json()
                if data.get('ok'):
                    return data['result'].get('message_id')
        except Exception as e:
            logger.error(f"Gagal kirim Telegram: {e}")
        return None

    def delete_tg_message(self, message_id):
        if not self.tg_token or not self.tg_chat_id or not message_id:
            return
        try:
            url = f"https://api.telegram.org/bot{self.tg_token}/deleteMessage"
            requests.post(url, json={
                "chat_id": self.tg_chat_id,
                "message_id": message_id
            }, timeout=5)
        except Exception:
            pass

    def delete_tg_messages(self, message_ids):
        if not message_ids:
            return
        for mid in message_ids:
            if mid:
                self.delete_tg_message(mid)

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

    def scan_and_attend(self, manual=False):
        now_str = get_wib_str()
        self.last_scan_time = now_str

        if not self.ensure_valid_session():
            return "❌ Gagal mengautentikasi ke SSO PENS."

        self.update_cache()
        if not self.courses_cache:
            return "⚠️ Data mata kuliah kosong atau gagal dimuat."

        found_open = 0
        results = []

        for c in self.courses_cache:
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
                if pres_resp.status_code == 200:
                    pres_data = pres_resp.json()
                    key = self.extract_active_key(pres_data)

                    if key:
                        found_open += 1
                        if key in self.attended_keys:
                            results.append(f"ℹ️ <b>{mk_nama}</b>: Presensi terbuka & sudah tercatat.")
                            continue

                        logger.info(f"⚡ Presensi Terbuka: {mk_nama} (Key: {key})")
                        payload = {
                            "kuliah": k_id,
                            "jenis_schema": schema,
                            "mahasiswa": self.user_info.get('nomor'),
                            "key": key,
                            "kuliah_asal": kuliah_asal
                        }
                        submit_resp = self.session.post('https://ethol.pens.ac.id/api/presensi/mahasiswa', json=payload, timeout=10)

                        if submit_resp.status_code == 200:
                            res_json = submit_resp.json()
                            pesan = res_json.get('pesan', 'Berhasil')

                            if res_json.get('sukses') or "sudah" in str(pesan).lower():
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

    def format_status_text(self):
        nama = self.user_info.get('nama', 'N/A') if self.user_info else 'Belum login'
        nrp = self.user_info.get('nipnrp', 'N/A') if self.user_info else '-'
        now_str = get_wib_str()

        now_wib = get_wib_now()
        time_val = now_wib.hour + now_wib.minute / 60.0

        if self.is_cooldown_active_today():
            scanner_status = f"🟡 Cooldown ({self.cooldown_date})"
            scanner_sub = "💤 Jeda s/d 00:00 WIB"
            jadwal_relogin = "Auto re-login esok hari (00:00 WIB)"
            aktivitas = "Istirahat (monitoring jeda)"
        elif self.force_siaga:
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

        return (
            "<b>┌─ DATA MAHASISWA ─────────────────</b>\n"
            f"│ Mahasiswa      : {nama}\n"
            f"│ NRP            : <code>{nrp}</code>\n"
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
        if not stats:
            return "Gagal memuat rekapitulasi kehadiran dari server ETHOL."

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
            "<b>REKAPITULASI KEHADIRAN RESMI</b>\n\n"
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
        if not tasks:
            return "<b>DAFTAR TUGAS KULIAH</b>\n\nSemua tugas semester ini telah dikumpulkan atau tidak ada tugas aktif."

        txt = "<b>DAFTAR TUGAS PENDING (BELUM DIKUMPULKAN):</b>\n\n"
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
        if not self.schedule_cache:
            return "Data jadwal perkuliahan belum tersedia."

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

        txt = f"<b>JADWAL KULIAH (Semester {self.semester_aktif}/{self.tahun_aktif}):</b>\n"
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

    def handle_tg_command(self, cmd, user_msg_id=None):
        c = cmd.lower().strip()
        logger.info(f"Menerima perintah Telegram: {cmd}")
        credit = "\n\n✦ <b>Creator : Gungna</b>"

        # Hapus pesan-pesan interaksi perantara sebelumnya
        self.delete_tg_messages(self.last_interaction_msg_ids)
        self.last_interaction_msg_ids = []

        if c in ['/start', '/help', 'help', '/menu']:
            # Jika user panggil /start baru, bersihkan banner menu utama lama & pesan input user
            if self.main_menu_msg_id:
                self.delete_tg_message(self.main_menu_msg_id)
                self.main_menu_msg_id = None
            if user_msg_id:
                self.delete_tg_message(user_msg_id)

            help_text = (
                "<b>PANDUAN PERINTAH KON-THOL:</b>\n\n"
                "⚡ /scan atau /absen - Scan presensi seketika\n"
                "📅 /jadwal - Jadwal perkuliahan mingguan\n"
                "📊 /rekap - Rekapitulasi kehadiran semester\n"
                "📝 /tugas - Daftar tugas pending & tautan\n"
                "📜 /log - Riwayat catatan log aktivitas\n"
                "ℹ️ /status - Status bot & sesi login SSO\n"
                "💤 /cooldown - Istirahatkan scanner hari ini\n"
                "⚡ /resume - Batalkan cooldown & kembali siaga\n"
                "🔄 /relogin - Sinkronisasi ulang sesi SSO PENS\n"
                + credit
            )
            self.main_menu_msg_id = self.send_tg_photo(help_text)
            return

        # Untuk slash command aktif (terakhir), catat input user dan hasil jawaban
        current_batch = []
        if user_msg_id:
            current_batch.append(user_msg_id)

        if c in ['/scan', '/absen', 'scan', 'absen']:
            prog_id = self.send_tg("⏳ Sedang memindai presensi di server ETHOL...")
            res = self.scan_and_attend(manual=True)
            res_id = self.send_tg(f"{res}{credit}")
            if prog_id:
                self.delete_tg_message(prog_id)
            if res_id:
                current_batch.append(res_id)
        elif c in ['/jadwal', '/matkul', 'jadwal']:
            res_id = self.send_tg(f"{self.format_jadwal_text()}{credit}")
            if res_id: current_batch.append(res_id)
        elif c in ['/rekap', 'rekap']:
            res_id = self.send_tg(f"{self.format_rekap_detail()}{credit}")
            if res_id: current_batch.append(res_id)
        elif c in ['/tugas', 'tugas']:
            res_id = self.send_tg(f"{self.format_tugas_text()}{credit}")
            if res_id: current_batch.append(res_id)
        elif c in ['/log', '/logs', 'log']:
            res_id = self.send_tg(f"{self.get_raw_logs(15)}{credit}")
            if res_id: current_batch.append(res_id)
        elif c in ['/status', 'status']:
            res_id = self.send_tg(f"{self.format_status_text()}{credit}")
            if res_id: current_batch.append(res_id)
        elif c in ['/cooldown', 'cooldown']:
            _, msg = self.activate_cooldown()
            res_id = self.send_tg(f"💤 {msg}{credit}")
            if res_id: current_batch.append(res_id)
        elif c in ['/resume', '/siaga', 'resume', 'siaga']:
            _, msg = self.deactivate_cooldown()
            res_id = self.send_tg(f"⚡ {msg}{credit}")
            if res_id: current_batch.append(res_id)
        elif c in ['/relogin', 'relogin']:
            prog_id = self.send_tg("🔄 Melakukan otentikasi ulang CAS SSO...")
            if self.login_cas(notify_on_fail=False):
                res_id = self.send_tg(f"✅ Berhasil login ulang ke SSO PENS!{credit}")
            else:
                res_id = self.send_tg(f"❌ Gagal login ulang ke SSO PENS.{credit}")
            if prog_id:
                self.delete_tg_message(prog_id)
            if res_id:
                current_batch.append(res_id)
        else:
            res_id = self.send_tg(f"Perintah tidak dikenal: <code>{html.escape(cmd)}</code>. Ketik /help untuk panduan.{credit}")
            if res_id: current_batch.append(res_id)

        self.last_interaction_msg_ids = current_batch

    def run_auto_loop(self, interval=120):
        logger.info(f"Scanner background aktif (interval {interval}s)...")
        while True:
            try:
                now_wib = get_wib_now()
                time_val = now_wib.hour + now_wib.minute / 60.0

                if self.is_cooldown_active_today():
                    time.sleep(30)
                    continue

                if not self.force_siaga and (time_val >= 21.5 or time_val < 4.0):
                    # Istirahat Malam: jam 21:30 - 04:00 (sebelum subuh)
                    # Tetap lakukan scan berkala santai tiap 5 menit (bukan sleep kosong),
                    # sehingga jika tiba-tiba ada absensi malam dibuka tetap ter-cover otomatis!
                    self.check_notifications_trigger()
                    self.scan_and_attend(manual=False)
                    time.sleep(300)
                    continue

                self.check_notifications_trigger()
                self.scan_and_attend(manual=False)
            except Exception as e:
                logger.error(f"Error pada auto loop: {e}")
            time.sleep(interval)

    def run_tg_listener(self):
        if not self.tg_token:
            return
        logger.info("Telegram Bot Listener aktif...")
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

                        if self.tg_chat_id and chat_id != self.tg_chat_id:
                            continue

                        if text.startswith('/'):
                            self.handle_tg_command(text, user_msg_id=msg_id)
            except Exception:
                time.sleep(3)

    def execute_cli(self, command):
        """Eksekusi perintah dari Terminal / CLI (mencetak teks bersih tanpa tag HTML)."""
        cmd = command.lower().strip()
        credit = "\n\n✦ Creator : Gungna"
        if cmd in ['scan', 'absen']:
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
