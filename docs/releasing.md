# Build otomatis dan rilis

Workflow `.github/workflows/build.yml` berjalan pada setiap push branch, pull
request, push tag `v*`, dan melalui **Actions > Build and release > Run workflow**.
Pilihan manual tersedia setelah workflow masuk ke default branch.

Versi Go mengikuti `go.mod`. Kedua target menerapkan patch go-text
(`patches/apply.sh`), lalu menjalankan pemeriksaan format, `go mod verify`,
`go vet ./...`, `go test ./...`, dan `go build ./...` sebelum membangun aplikasi:

| File | Isi |
| --- | --- |
| `WazzapClients-Setup.exe` | Installer Windows (Inno Setup, `installer/wazzap.iss`) |
| `WazzapClients-windows-amd64.exe` | Aplikasi Windows portable, tanpa jendela console; juga file yang diunduh oleh tombol update |
| `WazzapClients-linux-amd64` | Executable Linux |
| `SHA256SUMS`, `SHA256SUMS.sig` | Checksum semua file di atas, dan tanda tangan ed25519-nya (hanya di release) |
| `LICENSE`, `THIRD_PARTY_NOTICES.md` | Lisensi aplikasi (MPL-2.0) dan lisensi semua software pihak ketiga di dalamnya (hanya di release) |

CI juga menjalankan `go run ./cmd/notices -check`, yang gagal kalau
`THIRD_PARTY_NOTICES.md` tidak lagi cocok dengan dependensi atau aset yang dibundel.
Jalankan `go run ./cmd/notices` setelah mengubah dependensi, lalu commit hasilnya.

Unduh hasil dari bagian **Artifacts** pada run yang berhasil. Artefak disimpan
selama 14 hari; aset yang sudah dilampirkan ke GitHub Release tetap tersimpan.

Linux dibangun di Ubuntu 22.04 dengan CGO serta dukungan X11/Wayland. Paket ini
bukan binary statis atau AppImage; komputer tujuan memerlukan lingkungan desktop
dan library runtime Gio (X11/Wayland, xkbcommon, EGL/GLES). Lihat
[dependensi Linux Gio](https://gioui.org/doc/install/linux). macOS, ARM64, dan
code signing Windows belum disediakan oleh workflow ini.

## Installer Windows

Installer memasang aplikasi untuk user yang sedang login saja, ke
`%LocalAppData%\Programs\WazzapClients`, tanpa meminta hak admin. Installer membuat
shortcut di Start Menu (opsional di Desktop), menyalin `LICENSE.txt` dan
`THIRD_PARTY_NOTICES.txt` ke folder aplikasi, dan mendaftarkan uninstaller di
**Settings > Apps**. Uninstaller menutup aplikasi, menghapus entri registry yang
dibuat aplikasi sendiri (notifikasi dan start at login), lalu menanyakan apakah
chat dan sesi WhatsApp (`%AppData%\WazzapClients`) ikut dihapus. Defaultnya tidak.

Untuk membangunnya secara lokal, pasang [Inno Setup 6](https://jrsoftware.org/isinfo.php),
build exe ke `bin/dist/WazzapClients-windows-amd64.exe`, lalu jalankan
`iscc /DAppVersion=0.10.0 installer\wazzap.iss`.

## Update dari dalam aplikasi

**Settings > Help** menampilkan versi aplikasi dan tombol **Check for updates**.
Aplikasi tidak menghubungi GitHub sampai tombol itu ditekan. Kalau ada versi yang
lebih baru, tombol yang sama mengunduh exe untuk sistem ini, mencocokkannya dengan
`SHA256SUMS`, memeriksa tanda tangan `SHA256SUMS.sig` dengan kunci publik di
`internal/update`, menukar exe yang sedang berjalan, lalu me-restart aplikasi.

- Hanya build dari tag (`-X main.version=v1.2.3`) yang bisa update. Build lain,
  dan prerelease seperti `v1.2.3-rc1`, adalah development build.
- Yang dicek adalah *latest release* GitHub, jadi draft dan prerelease tidak
  pernah ditawarkan.
- Update butuh folder exe yang bisa ditulis. Installer memasang ke folder seperti
  itu; untuk versi portable, letakkan exe di folder milik user.

### Kunci tanda tangan

Job release menandatangani `SHA256SUMS` dengan `cmd/signrelease` memakai secret
`RELEASE_SIGNING_KEY` (seed ed25519 dalam base64). Tanpa secret itu job release
gagal. Kunci publiknya ada di `publicKey` (`internal/update/update.go`).

Simpan cadangan kunci privat di tempat aman. Kalau kunci hilang atau bocor, buat
kunci baru (`go run ./cmd/signrelease -keygen <file>`, yang mencetak kunci publik
barunya), ganti `publicKey` dan secret-nya. Versi lama aplikasi tidak akan mau
memasang rilis yang ditandatangani kunci baru, jadi penggunanya harus mengunduh
satu rilis itu secara manual.

## Membuat rilis

1. Commit dan push source beserta workflow ke repository GitHub.
2. Pada commit yang akan dirilis, buat dan push tag versi baru, misalnya:

   ```sh
   git tag -a v0.1.0 -m "WazzapClients v0.1.0"
   git push origin v0.1.0
   ```

3. Setelah kedua build berhasil, workflow membuat **draft release** dengan semua
   file di atas dan release notes otomatis.
4. Buka **Releases**, periksa draft, sunting catatan rilis (dan tandai prerelease
   bila diperlukan), lalu pilih **Publish release**. Baru setelah itu aplikasi
   menawarkannya sebagai update.

Hanya job release yang memiliki izin `contents: write`. Kebijakan
repository/organisasi harus mengizinkan GitHub Actions membuat release.

Menjalankan ulang run tag memperbarui aset selama release masih draft. Aset
release yang telah dipublikasikan tidak ditimpa; gunakan tag versi baru.
Run manual dan push branch hanya menghasilkan artefak, tanpa membuat release.

Untuk memeriksa unduhan di Linux, simpan file dan `SHA256SUMS` dalam satu
direktori, lalu jalankan `sha256sum --check --ignore-missing SHA256SUMS`. Di
PowerShell, gunakan `Get-FileHash .\WazzapClients-Setup.exe -Algorithm SHA256`
dan cocokkan hasilnya dengan entri di `SHA256SUMS`.
