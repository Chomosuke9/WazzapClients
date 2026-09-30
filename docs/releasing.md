# Build otomatis dan rilis

Workflow `.github/workflows/build.yml` berjalan pada setiap push branch, pull
request, push tag `v*`, dan melalui **Actions > Build and release > Run workflow**.
Pilihan manual tersedia setelah workflow masuk ke default branch.

Versi Go mengikuti `go.mod`. Kedua target menjalankan pemeriksaan format,
`go mod verify`, `go vet ./...`, `go test ./...`, dan `go build ./...` sebelum
mengemas aplikasi:

| Target | Paket | Isi |
| --- | --- | --- |
| Windows amd64 | `WazzapClients-windows-amd64.zip` | `WazzapClients.exe`, tanpa jendela console |
| Linux amd64 | `WazzapClients-linux-amd64.tar.gz` | executable `wazzapclients` |

Unduh hasil dari bagian **Artifacts** pada run yang berhasil. Artefak disimpan
selama 14 hari; aset yang sudah dilampirkan ke GitHub Release tetap tersimpan.
Paket hanya berisi aplikasi, tanpa database sesi atau data WhatsApp lokal.

Linux dibangun di Ubuntu 22.04 dengan CGO serta dukungan X11/Wayland. Paket ini
bukan binary statis atau AppImage; komputer tujuan memerlukan lingkungan desktop
dan library runtime Gio (X11/Wayland, xkbcommon, EGL/GLES). Lihat
[dependensi Linux Gio](https://gioui.org/doc/install/linux). macOS, ARM64, installer,
dan code signing belum disediakan oleh workflow ini.

## Membuat rilis

1. Commit dan push source beserta workflow ke repository GitHub. Pastikan seluruh
   file source yang diperlukan sudah terlacak Git, termasuk
   `cmd/screenshot/film.go` yang digunakan oleh `cmd/screenshot/main.go`.
2. Pada commit yang akan dirilis, buat dan push tag versi baru, misalnya:

   ```sh
   git tag -a v0.1.0 -m "WazzapClients v0.1.0"
   git push origin v0.1.0
   ```

3. Setelah kedua build berhasil, workflow membuat **draft release** dengan kedua
   paket, `SHA256SUMS`, dan release notes otomatis.
4. Buka **Releases**, periksa draft, sunting catatan rilis (dan tandai prerelease
   bila diperlukan), lalu pilih **Publish release**.

Workflow menggunakan `GITHUB_TOKEN` bawaan; tidak memerlukan PAT atau secret
tambahan. Hanya job release yang memiliki izin `contents: write`. Kebijakan
repository/organisasi harus mengizinkan GitHub Actions membuat release.

Menjalankan ulang run tag memperbarui aset selama release masih draft. Aset
release yang telah dipublikasikan tidak ditimpa; gunakan tag versi baru.
Run manual dan push branch hanya menghasilkan artefak, tanpa membuat release.

Untuk memeriksa unduhan di Linux, simpan kedua paket dan `SHA256SUMS` dalam satu
direktori, lalu jalankan `sha256sum --check SHA256SUMS`. Di PowerShell, gunakan
`Get-FileHash .\WazzapClients-windows-amd64.zip -Algorithm SHA256` dan cocokkan
hasilnya dengan entri di `SHA256SUMS`.
