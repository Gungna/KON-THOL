#!/usr/bin/env python3
"""
KON-THOL — Interactive Setup Wizard
Memudahkan konfigurasi akun SSO PENS dan Telegram Bot tanpa perlu mengedit file JSON secara manual.
"""

import os
import sys
import json
import getpass
import requests
from bs4 import BeautifulSoup
import urllib.parse

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
CRED_FILE = os.path.join(BASE_DIR, "credentials.json")

def print_banner():
    print("=" * 60)
    print("   KON-THOL — Setup Wizard Kredensial & Bot")
    print("   Kawan Otomasi dan Notifikasi untuk E-THOL PENS")
    print("=" * 60)
    print("Wizard ini akan memandu Anda menyiapkan akun SSO dan Telegram.")
    print("Data disimpan secara lokal di perangkat Anda (credentials.json).\n")

def test_sso_login(username, password):
    print("\n[*] Menguji autentikasi SSO PENS...")
    session = requests.Session()
    session.headers.update({
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36'
    })

    try:
        resp = session.get('https://ethol.pens.ac.id/api/auth/cas-redirect', timeout=15)
        soup = BeautifulSoup(resp.text, 'html.parser')
        form = soup.find('form', id='fm1')
        if not form:
            print("[!] Formulir CAS SSO tidak ditemukan.")
            return False, None

        action = form.get('action')
        post_url = urllib.parse.urljoin(resp.url, action)

        form_data = {inp.get('name'): inp.get('value', '') for inp in form.find_all('input') if inp.get('name')}
        form_data['username'] = username
        form_data['password'] = password
        form_data['_eventId'] = 'submit'
        form_data['submit'] = 'LOGIN'

        session.post(post_url, data=form_data, allow_redirects=True, timeout=15)

        val_resp = session.get('https://ethol.pens.ac.id/api/auth/validasi-token', timeout=10)
        if val_resp.status_code == 200:
            user_data = val_resp.json()
            nama = user_data.get('nama', 'Mahasiswa')
            nrp = user_data.get('nipnrp', '-')
            return True, f"{nama} (NRP: {nrp})"
        else:
            return False, "Kombinasi email atau password salah."
    except Exception as e:
        return False, f"Terjadi kendala koneksi: {e}"

def test_telegram_bot(token, chat_id):
    print("[*] Menguji koneksi ke Bot Telegram...")
    try:
        # Cek validitas bot token
        me_url = f"https://api.telegram.org/bot{token}/getMe"
        me_res = requests.get(me_url, timeout=10).json()
        if not me_res.get('ok'):
            return False, "Token Bot Telegram tidak valid. Periksa kembali token dari @BotFather."

        bot_username = me_res.get('result', {}).get('username')

        # Kirim pesan tes ke chat_id
        send_url = f"https://api.telegram.org/bot{token}/sendMessage"
        payload = {
            "chat_id": chat_id,
            "text": "🎉 <b>KON-THOL BERHASIL TERHUBUNG!</b>\n\nSetup wizard telah berhasil mengonfigurasi bot ini dengan akun Telegram Anda.",
            "parse_mode": "HTML"
        }
        send_res = requests.post(send_url, json=payload, timeout=10).json()
        if not send_res.get('ok'):
            return False, f"Token valid (@{bot_username}), tetapi Chat ID salah atau Anda belum menekan /start pada bot."

        return True, f"@{bot_username}"
    except Exception as e:
        return False, f"Kendala koneksi ke Telegram API: {e}"

def main():
    print_banner()

    # 1. Input Akun SSO PENS
    username = input("1. Masukkan Email SSO PENS (cth: nama@student.pens.ac.id): ").strip()
    while not username:
        username = input("   Email tidak boleh kosong: ").strip()

    password = getpass.getpass("2. Masukkan Password SSO PENS: ").strip()
    while not password:
        password = getpass.getpass("   Password tidak boleh kosong: ").strip()

    sso_ok, sso_msg = test_sso_login(username, password)
    if sso_ok:
        print(f"   [+] Login SSO Berhasil! Mahasiswa: {sso_msg}")
    else:
        print(f"   [!] Peringatan SSO: {sso_msg}")
        pilih = input("   Lanjutkan penyimpanan meski login belum terverifikasi? (y/n): ").lower()
        if pilih != 'y':
            print("Setup dibatalkan. Silakan periksa kembali email & password Anda.")
            sys.exit(0)

    print("-" * 60)

    # 2. Input Kredensial Telegram (Opsional / Fleksibel)
    print("Panduan Singkat Telegram:")
    print("• KON-THOL paling nyaman dipakai dengan bot Telegram pribadi.")
    print("• Token Bot didapatkan gratis dari @BotFather di Telegram.")
    print("• Chat ID Anda bisa dilihat dengan mengirim pesan ke @userinfobot.")
    print("• TIPS: Jika Anda hanya ingin menjalankan script di terminal (tanpa Telegram), tekan Enter langsung.\n")

    tg_token = input("3. Masukkan Token Bot Telegram (Kosongkan jika tanpa Telegram): ").strip()
    tg_chat_id = ""

    if tg_token:
        tg_chat_id = input("4. Masukkan Chat ID Telegram Anda: ").strip()
        while not tg_chat_id:
            tg_chat_id = input("   Chat ID tidak boleh kosong jika token diisi: ").strip()

        tg_ok, tg_msg = test_telegram_bot(tg_token, tg_chat_id)
        if tg_ok:
            print(f"   [+] Bot Telegram Terhubung: {tg_msg}")
        else:
            print(f"   [!] Peringatan Telegram: {tg_msg}")
            pilih = input("   Lanjutkan penyimpanan konfigurasi? (y/n): ").lower()
            if pilih != 'y':
                print("Setup dibatalkan.")
                sys.exit(0)
    else:
        print("   [*] Mode CLI / Standalone dipilih. Bot akan memantau presensi tanpa mengirim notifikasi Telegram.")


    print("-" * 60)

    # 3. Simpan ke credentials.json
    creds = {
        "username": username,
        "password": password,
        "telegram_token": tg_token,
        "telegram_chat_id": tg_chat_id
    }

    with open(CRED_FILE, 'w', encoding='utf-8') as f:
        json.dump(creds, f, indent=2)

    print("\n[OK] Kredensial berhasil disimpan di:")
    print(f"     {CRED_FILE}")
    print("\nLangkah selanjutnya:")
    print("• Jalankan bot langsung: python3 ethol_autopresence_v2.py")
    print("• Buka bot Anda di Telegram dan ketik /start untuk membuka menu!")
    print("=" * 60)

if __name__ == '__main__':
    main()
