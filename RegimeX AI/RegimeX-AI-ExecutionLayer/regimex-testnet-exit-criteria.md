# RegimeX AI: Testnet Exit Criteria (v16)

Status proyek per akhir audit desain:

| Area | Status |
|---|---|
| SafeMode, concurrency, persistence, advisory lock | Closed (pending empirical validation) |
| Signing parity (offline) | Lulus (2066 subtest vs SDK 0.24.0). Server acceptance belum diuji, lihat Phase 1.5 |
| MoveStopLoss correctness | Diperbaiki dan diuji unit (trigger order, guard marketability, ratchet, atomic move). Belum diuji di testnet nyata |
| Position identity | ARCHITECTURAL RISK REMAINING, bukan kuning biasa: bisa memaksa perubahan desain recovery (EntryID, reopen prevention) jika tidak ada field identitas stabil |
| Stop-order classification | Belum tervalidasi |
| Timestamp semantics | Belum tervalidasi |
| Real money deployment | Belum |

Prinsip: setiap asumsi desain yang tersisa harus menjadi bukti empiris terdokumentasi. Fase tidak boleh dilompat. Perubahan v16: dua klaim dipersempit presisinya di komentar kode (anti-collision arsip ternyata unik-per-proses, bukan unik-global; normalizeCloid memvalidasi "format sesuai dokumentasi", bukan "terbukti diterima server"), tanpa perubahan logika. Perubahan v15: hexDecode kustom (fmt.Sscanf per-byte) diganti encoding/hex standar library, fungsi lokal yang bentrok nama di-rename (decodePrivateKeyHex); ditambah stress test 1000 goroutine paralel untuk archiveResponseTo (lulus bersih dengan -race, 0 collision, 0 data race), dengan satu false-failure di percobaan pertama yang terlacak ke bug test sendiri (format string pembanding tidak cocok keluaran json.MarshalIndent), bukan bug kode. Perubahan v14: KOREKSI dua defect nyata dari v13 (validasi cloid hanya cek panjang, meloloskan karakter non-hex; nama arsip belum benar-benar dijamin unik, resolusi milidetik bisa collision dan overwrite senyap), plus dua perbaikan minor (test kegagalan arsip dirombak total supaya memaksa kegagalan nyata alih-alih mencocokkan string; timeout<=0 ditolak eksplisit karena berarti unlimited di http.Client); istilah "fail-closed" untuk --require-archive dipersempit, itu fail-closed terhadap kelanjutan test run lokal, bukan terhadap exchange action yang sudah terlanjur terkirim. Perubahan v13: empat item diturunkan eksplisit dari "selesai" menjadi "perlu verifikasi tambahan" (validasi cloid, kalibrasi timeout, cakupan test build-vs-acceptance, kebijakan kegagalan arsip); timeout HTTP jadi flag --timeout (bukan konstanta tetap) dengan status eksplisit belum dikalibrasi empiris; kebijakan fail-open/fail-closed untuk kegagalan arsip diimplementasikan via flag --require-archive. Perubahan v12: KOREKSI blocker nyata (cancel_by_cloid tercantum di help text go/cmd/send tapi tidak diimplementasikan, membuat testnet tooling sebenarnya belum lengkap untuk 4 action seperti diklaim); ditambahkan beserta validasi cloid dan test regresi yang memverifikasi seluruh action yang diiklankan help text benar-benar bisa dibangun; HTTP client sekarang punya timeout eksplisit (10 detik, bukan DefaultClient tanpa batas); lampiran Phase 1.6 diperbarui, sebelumnya salah menyatakan MoveStopLoss belum diperbaiki padahal sudah. Perubahan v11: KOREKSI bug nyata dari perbaikan v10 sendiri (body HTTP asli bocor karena tidak pernah ditutup setelah resp.Body ditimpa); ownership sekarang transfer eksplisit (archiveResponse menutup body asli sebelum memasang replay body), plus error ReadAll/Marshal yang sebelumnya dibuang kini direkam eksplisit di arsip. Perubahan v10: tiga perbaikan pada go/cmd/send (kontrak ownership resp.Body dipertegas, nama field arsip dikonsistenkan jadi response_body_raw di kedua jalur, validasi hex key menolak panjang ganjil/kosong secara eksplisit). Perubahan v9: go/cmd/send sekarang mengarsipkan request dan response mentah secara otomatis (raw_responses/) untuk setiap pengiriman, menjadi bukti forensik untuk membedakan bug RegimeX dari perubahan perilaku exchange di masa depan. Perubahan v8: metrik reconciliation duration (p50/p95/p99/max) ditambahkan ke soak test, untuk menangkap regresi performa recovery yang lambat seiring pertumbuhan data, tidak terlihat dari metrik goroutine/memori/DB yang sudah ada. Perubahan v7: toFloat mendukung json.Number (future-proofing untuk decoder.UseNumber, belum relevan di jalur kode saat ini). Perubahan v6: tiga perbaikan robustness pada cancel_idempotency.go dan verify.go (case-insensitive marker matching, korelasi oid-via-cloid untuk membedakan cancel sungguhan dari modify, perbandingan nilai sadar-tipe dengan epsilon untuk float). Perubahan v5: Position Identity dinaikkan jadi architectural risk, acceptance criteria field-by-field (Phase 1.5), klasifikasi cancel idempotency, snapshot metrik soak per jam. Perubahan v3: hasil Fase 1 offline, temuan terhadap kode awal, arsip payload mentah, klasifikasi error cancel. Perubahan v2: boundary corpus, critical vs optional fields, partial-failure network, metrik soak numerik, dan tugas eksplisit untuk position identity.

---

## Phase 1: Signing Parity

**Tujuan:** signature Go identik byte-for-byte dengan SDK Python resmi untuk seluruh ruang input yang relevan.

**Artefak (repo, permanen):** `parity/` berisi `gen_golden.py` (33 kasus batas), `gen_random.py` (1000 payload acak berseed 20260928, lintas order/batchModify/cancel/cancelByCloid), dua file corpus JSON, dan `go/` (signer, builder, test). Corpus adalah golden corpus permanen: setelah refactor serializer atau upgrade library crypto/SDK, `go test` langsung mendeteksi regresi.

**Metode:** tiap kasus diuji lewat dua jalur (encoder_only dari action_json berurutan, dan builder_from_spec dari parameter mentah). Kegagalan melaporkan layer pertama yang berbeda: msgpack_hex, msgpack_sha256, nonce_hex, vault_expires_suffix_hex, connection_id_hex, phantom_source, eip712_digest_hex, signature r/s/v.

**Hasil offline (2066 subtest, 0 gagal):**
- 33 kasus batas: qty dan harga ekstrem, integer vs desimal, trailing zero, lebar integer msgpack di batas asset index (0, 127, 128, 255, 256, 65535, 65536, 100000), trigger sl/tp, cloid ada/kosong/campuran, multi-order, grouping, builder, batchModify (oid dan cloid), cancel, cancelByCloid, vault, expiresAfter, mainnet vs testnet, nonce kecil dan besar.
- 1000 payload acak: order 554, batchModify 181, cancel 132, cancelByCloid 133.

**Riwayat kegagalan (dicatat, bukan disembunyikan):** run pertama corpus acak menghasilkan 96 kegagalan dari 2000 subtest. Analisis: semuanya action order, jalur builder_from_spec, layer pertama msgpack_hex, dan himpunannya sama persis dengan semua kasus yang memakai builder berhuruf campuran (96 dari 96 kasus builder gagal, 0 dari kasus tanpa builder). Akar masalah ada di GENERATOR corpus: SDK melakukan `.lower()` pada alamat builder di `Exchange.bulk_orders` sebelum membangun action, sedangkan generator memanggil `order_wires_to_order_action` langsung. Builder Go sudah meniru SDK dengan benar. Generator diperbaiki dan seluruh test diulang. Pelajaran: perbedaan bisa berasal dari harness, bukan hanya dari kode produksi; selalu buktikan dengan himpunan kegagalan, bukan asumsi.

**Uji mutasi (membuktikan harness menangkap bug nyata):** meniru bug kode RegimeX awal, yaitu format harga fixed `%.5f`, `source` "a" hardcoded, dan integer msgpack fixed-width, masing-masing membuat test gagal dengan laporan layer yang tepat.

**Sisa Fase 1 (belum selesai):**
1. Uji penerimaan di server testnet: kirim order, modify, dan cancel bertanda tangan Go, pastikan diterima (bukan "invalid signature"). Parity offline tidak membuktikan ini.
2. Pin versi SDK (0.24.0) dan jalankan parity di CI; regenerasi corpus saat SDK di-upgrade.
3. Format r dan s dikirim sebagai hex minimal tanpa leading zero (sama dengan SDK); konfirmasi diterima server.

**Exit criteria:** parity offline 100% (tercapai), DITAMBAH penerimaan server testnet untuk keempat jenis action.

---

## Phase 1.5: Server Acceptance

**Kenapa fase terpisah:** signature yang identik dengan SDK 0.24.0 secara offline tidak membuktikan server testnet menerimanya. SDK bisa berbeda dari perilaku server saat ini, atau ada field yang belum masuk corpus.

**Prosedur:** untuk tiap action (order, batchModify, cancel, cancelByCloid), kirim payload bertanda tangan Go ke testnet dan verifikasi: diterima (bukan "invalid signature" atau error validasi), dan state exchange berubah sesuai harapan (order muncul di openOrders, modify mengganti oid, cancel menghilangkan order).

**Acceptance bukan sekadar HTTP 200 (wajib, bukan opsional):** server menerima request tidak membuktikan order yang tersimpan sama dengan intent yang dikirim. Kegagalan senyap yang mungkin lolos cek status saja: `reduceOnly` hilang, `triggerPx` dibulatkan server ke nilai berbeda, trigger market berubah jadi trigger limit atau sebaliknya. Untuk tiap action, setelah POST, WAJIB query `openOrders` dan bandingkan field demi field terhadap intent yang dikirim, bukan hanya cek status sukses.

`go/cmd/send/verify.go` menyediakan `VerifyOrderIntent` untuk ini: menerima response order mentah sebagai map (nama field belum diasumsikan), sebuah `ExpectedIntent`, dan `fieldMap` yang memetakan konsep (reduce_only, is_buy, sz, trigger_px, is_market, tpsl) ke nama field asli. Sebelum `fieldMap` diisi dari hasil Fase 2, setiap pengecekan otomatis dilaporkan sebagai mismatch ("FIELD MAPPING BELUM DIISI"), bukan diam-diam lolos, sehingga verifikasi field-by-field tidak bisa terlewat begitu saja (diuji: `TestVerifyOrderIntent_EmptyFieldMapReportsMissing`).

**Perbaikan robustness v6:** perbandingan nilai sekarang sadar-tipe (`valuesMatch`), bukan membandingkan representasi string JSON. `want=1` (int) vs `got=1.0` (float64, hasil wajar dari `json.Unmarshal`) tidak lagi keliru dianggap beda; nilai float dibandingkan dengan epsilon (1e-9) sehingga presisi representasi JSON (`0.1` vs `0.1000000000001`) tidak menghasilkan noise mismatch palsu; string numerik dari exchange (harga/size sering dikirim sebagai string, mis. `"0.001"`) tetap terbandingkan dengan benar. Perbedaan nilai yang nyata tetap terdeteksi (diuji: `TestValuesMatch_GenuinelyDifferentStillDetected`). `toFloat` juga menerima `json.Number` (diuji: `TestValuesMatch_JSONNumberSupported`), antisipasi kalau parser suatu saat berpindah dari `json.Unmarshal` biasa ke `decoder.UseNumber()` seperti yang sudah dipakai di `signing_test.go` untuk menjaga urutan key; saat ini belum relevan karena `VerifyOrderIntent` masih menerima map hasil `json.Unmarshal` biasa (selalu `float64`).

**Arsip forensik otomatis (v12):** `go/cmd/send` sekarang menulis setiap pengiriman (sukses maupun gagal) ke `raw_responses/<action>_<timestamp>.json`, berisi request body, response body mentah (atau pesan error transport kalau request gagal sebelum dapat response), status HTTP, dan timestamp UTC, disimpan apa adanya tanpa diproses. Nama file memuat action dan timestamp sehingga tidak saling menimpa antar pemanggilan (diuji: `TestArchiveResponse_FilenameContainsActionAndIsUnique`). Arsip ditulis sebelum program bisa exit karena error, sehingga kegagalan request pun tetap terekam (diuji: `TestArchiveResponse_TransportErrorIsRecorded`). Body response dibaca ulang dan dikembalikan ke pembaca normal di `main()` setelah diarsipkan, sehingga pencetakan ke layar tetap berjalan seperti biasa (diuji: `TestArchiveResponse_DoesNotConsumeBodyForCaller`, ini menangkap bug nyata: percobaan pertama implementasi mengonsumsi body tanpa mengembalikannya, yang akan membuat response tercetak kosong di layar meski tersimpan benar di arsip).

Tujuan arsip ini: kalau Hyperliquid mengubah schema atau perilaku di masa depan, ada bukti request-response apa adanya untuk membedakan "bug RegimeX" dari "perubahan perilaku exchange", tanpa harus menebak dari log yang sudah diproses.

**Perbaikan v10 (offline/mock-validated, 42/42 test lulus):**
- **Ownership `resp.Body` dipertegas.** Sebelumnya ada `defer resp.Body.Close()` di dalam `archiveResponse` yang menutup Body ASLI (karena defer mengevaluasi nilai saat dibuat, bukan saat dieksekusi), sementara Body PENGGANTI yang dipakai caller tidak pernah ditutup eksplisit oleh siapa pun. Fungsional tidak berbahaya (`io.NopCloser.Close` no-op), tapi ownership ambigu. Dihapus; `archiveResponse` sekarang tidak pernah menutup body dalam kondisi apa pun, caller (`main()`) tetap satu-satunya pemilik lifecycle (diuji: `TestArchiveResponse_DoesNotCloseBody`).
- **Skema arsip dikonsistenkan.** Jalur sukses dan jalur transport-error sekarang sama-sama memakai nama field `response_body_raw` (bernilai string di jalur sukses, `null` di jalur error), bukan dua nama berbeda (`response_body_raw` vs `response_body`) untuk konsep yang sama (diuji: `TestArchiveResponse_TransportErrorIsRecorded` memverifikasi field lama tidak lagi muncul).
- **Validasi hex key eksplisit.** `hex()` sekarang menolak string hex dengan panjang ganjil atau kosong di awal fungsi, bukan diam-diam menghasilkan hasil parsing yang tidak jelas (diuji: `TestHex_OddLengthRejected`, `TestHex_EmptyRejected`, plus guard tambahan `TestHex_EvenButInvalidCharsRejected` untuk karakter non-hex).

**KOREKSI v11, bug nyata ditemukan pada perbaikan v10 sendiri (offline/mock-validated, 43/43 test lulus):**
- **Body HTTP asli bocor.** Perbaikan v10 menghapus SEMUA pemanggilan `Close()` di `archiveResponse` dengan asumsi "caller tetap pemilik, biar caller yang tutup". Ini salah: begitu `resp.Body` ditimpa dengan `io.NopCloser(bytes.NewReader(...))`, referensi ke body HTTP ASLI hilang total dari `resp.Body`. `defer resp.Body.Close()` di `main()` kemudian menutup REPLAY BODY (yang `Close()`-nya no-op), sementara koneksi HTTP asli tidak pernah benar-benar ditutup, kebocoran koneksi nyata (http.Client tidak bisa mengembalikan koneksi ke pool), bukan sekadar ambiguitas kontrak seperti yang dikira sebelumnya.
- **Fix: ownership sebagai transfer, bukan "tidak pernah close".** `archiveResponse` sekarang menyimpan referensi body asli (`originalBody := resp.Body`), membaca isinya, lalu MENUTUP body asli tepat di situ, SEBELUM memasang replay body ke `resp.Body`. Urutan eksplisit: body asli dibaca → body asli ditutup oleh `archiveResponse` → replay body dipasang → ownership replay body berpindah ke caller → caller membaca dan menutup replay body seperti biasa.
- **Test dirombak untuk membuktikan dua lifecycle terpisah**, bukan sekadar "pembacaan kedua masih jalan" (kelemahan test v10 yang ditunjukkan: test lama memakai `resp.Body.Close()` tunggal yang tidak bisa membedakan "body asli tertutup benar" dari "body pengganti kebetulan juga bisa ditutup"). `TestArchiveResponse_OriginalBodyClosedExactlyOnce` memakai `trackingBody` berpenghitung `Close()`, memverifikasi: body asli ditutup tepat 1 kali oleh `archiveResponse`, `resp.Body` setelahnya adalah objek berbeda dari body asli, caller tetap membaca payload yang sama, dan `Close()` caller terhadap replay body tidak mengubah hitungan `Close()` body asli.
- **Guard tambahan:** `TestArchiveResponse_ClosesOriginalEvenOnReadError` memverifikasi body asli tetap ditutup meski `ReadAll` gagal (mencegah early-return yang lupa menutup).
- **Error yang sebelumnya dibuang kini direkam eksplisit:** `io.ReadAll` dan `json.MarshalIndent` tidak lagi membuang error lewat `_`; kegagalan `ReadAll` dicatat sebagai field `read_error` di arsip (arsip forensik tidak boleh diam-diam parsial), kegagalan marshal dilaporkan ke stderr dengan jelas.

**KOREKSI v12, blocker nyata ditemukan: tooling belum lengkap untuk 4 action seperti diklaim.** Help text `go/cmd/send` mencantumkan `cancel_by_cloid`, tapi implementasinya sempat tidak ada di switch (jatuh ke `default: action tidak dikenal`), kontradiksi langsung dengan exit criteria fase ini. Diperbaiki: `case "cancel_by_cloid"` ditambahkan dengan flag `-cloid`, memakai `CancelSpec.Cloid` yang sudah ada di builder.

**Status diturunkan eksplisit, v13 (empat item berikut TIDAK lagi diklaim "selesai", melainkan "perlu verifikasi tambahan" sampai testnet nyata menjawabnya):**

1. **Validasi `cloid` = 32 karakter hex.** Bukan tebakan sembarangan, angka ini berasal dari dokumentasi resmi Hyperliquid (cloid = 128-bit hex, diverifikasi lewat pencarian terhadap dokumentasi saat merancang skema ownership). Tapi dokumentasi dan perilaku server bisa berbeda; validasi lokal yang lebih ketat dari yang sebenarnya diterima server bisa menghasilkan false-negative (exchange menerima, tool lokal menolak duluan). Wajib dikonfirmasi ulang terhadap respons server nyata di Fase 1.5, bukan diasumsikan identik dengan dokumentasi.

2. **Timeout HTTP (sebelumnya konstanta 10 detik, sekarang flag `-timeout`, default tetap 10 detik).** Nilai ini adalah starting point operasional, BUKAN hasil pengukuran p99 latency testnet nyata. Kalau server butuh >10 detik di kondisi tertentu (load tinggi), request yang sebenarnya berhasil bisa timeout duluan dan masuk jalur `CancelUnknown`/ambiguous. Nilai yang cocok untuk Fase 1.5 (acceptance manual, toleransi delay lebih besar) kemungkinan berbeda dari yang cocok untuk soak test atau recovery test (butuh deteksi gagal lebih cepat). Dijadikan flag, bukan konstanta, justru supaya kalibrasi ulang per fase tidak perlu ubah kode. Wajib dikalibrasi dari observasi nyata sebelum dipakai di luar Fase 1.5.

3. **`TestAllAdvertisedActions_BuildSuccessfully` membuktikan build path, BUKAN acceptance.** Test ini (ditambahkan v12) memverifikasi CLI → builder → signer berhasil untuk keempat action, menangkap regresi "tercantum di help text tapi implementasi hilang". Ia TIDAK membuktikan exchange menerima action tersebut. Klaim yang benar: "keempat action dapat dibangun dan ditandatangani", bukan "keempat action tervalidasi". Acceptance exchange sungguhan tetap sepenuhnya menunggu Fase 1.5 dijalankan terhadap testnet nyata.

4. **Kebijakan kegagalan arsip, sekarang eksplisit (v13).** Sebelumnya tidak terdefinisi apa yang terjadi kalau `archiveResponse` gagal menulis (disk penuh, permission, path tidak ada). Diputuskan dan diimplementasikan: DEFAULT fail-OPEN, kegagalan arsip tidak menghentikan program (karena request ke exchange sudah terlanjur punya efek nyata yang tidak bisa "dibatalkan" dengan menghentikan program setelahnya), tapi dilaporkan KERAS ke stderr dengan prefix `CRITICAL`. Flag `-require-archive` membalik ini jadi fail-CLOSED (exit 1) untuk sesi testnet formal yang menjadikan arsip lengkap sebagai syarat, sesuai exit criteria fase ini. Diuji: `TestArchiveFailureDetection_PrefixMatchesActualFailureReturn` memverifikasi deteksi kegagalan di `main()` konsisten dengan nilai yang benar-benar dikembalikan `archiveResponse` saat gagal.

**KOREKSI v14, dua defect nyata ditemukan pada v13 sendiri (offline/mock-validated, 56/56 test lulus):**

1. **Validasi `cloid` hanya mengecek panjang, bukan isi.** `len(c) != 32` meloloskan string 32 karakter yang bukan hex sama sekali (contoh konkret: 32 huruf `z`) ke dalam action yang dikirim ke server. Diperbaiki lewat `normalizeCloid`, yang mendekode seluruh string sebagai hex dan menolak kalau gagal, bukan cuma mengecek panjangnya. Tujuh test menutupi: karakter non-hex, hyphen di tengah, panjang salah (31/33), valid lowercase, valid uppercase (dinormalisasi ke lowercase), valid dengan prefix `0x`, dan kosong.

2. **Nama arsip belum benar-benar unik.** Resolusi timestamp sebelumnya hanya milidetik; dua request pada milidetik yang sama akan menghasilkan nama file identik dan `os.WriteFile` biasa menimpa tanpa pemberitahuan, bertentangan langsung dengan klaim "tidak saling menimpa". Diperbaiki tiga lapis: presisi timestamp dinaikkan ke nanodetik, nomor urut atomik per-proses ditambahkan sebagai pembeda tambahan (`nextArchiveSeq`), dan penulisan file memakai `O_EXCL` sehingga collision yang tersisa menjadi error eksplisit, bukan overwrite senyap. Diuji: dua panggilan dengan `sentAt` yang SENGAJA dibuat identik tetap menghasilkan file terpisah dengan isi masing-masing (`TestArchiveResponseTo_SequenceNumberPreventsCollisionInPractice`), dan percobaan collision paksa terbukti gagal tanpa mengubah isi file yang sudah ada (`TestArchiveResponseTo_CollisionIsExplicitErrorNotOverwrite`).

3. **Test kegagalan arsip dirombak total.** Test v13 (`TestArchiveFailureDetection_PrefixMatchesActualFailureReturn`) hanya mencocokkan string literal terhadap dirinya sendiri, tidak pernah memanggil `archiveResponse` sungguhan, overclaim terhadap apa yang diuji. `archiveResponse` dipecah jadi `archiveResponseTo(dir, ...)` yang menerima direktori sebagai parameter, sehingga test bisa menyuntik direktori yang benar-benar tidak bisa ditulis (file biasa menghalangi path, memaksa `ENOTDIR`) dan memverifikasi kegagalan NYATA terdeteksi (`TestArchiveResponseTo_FailsWhenDirIsUnwritable`). Detektor kegagalan disatukan jadi satu fungsi `isArchiveFailure`, dipakai `main()` dan test, supaya keduanya tidak bisa diam-diam berbeda.

4. **`-timeout=0` sebelumnya diam-diam berarti unlimited** (perilaku `http.Client` saat `Timeout<=0`), membatalkan invariant "network call tidak boleh menggantung tanpa batas" tanpa operator sadar. Ditolak eksplisit di awal `main()` dengan pesan jelas.

5. **Istilah "fail-closed" untuk `-require-archive` dipersempit.** Karena `archiveResponse` dipanggil SETELAH `httpClient.Post(...)`, kegagalan arsip terjadi setelah request ke exchange sudah terlanjur terkirim. "Fail-closed" di sini berarti menutup kelanjutan test run lokal dan exit criteria formal, BUKAN mencegah exchange action, yang memang tidak bisa dibatalkan lewat menghentikan program setelahnya. Komentar kode diperjelas supaya tidak menyesatkan reviewer bahwa order "dicegah terkirim".

**Perbaikan v15 (offline/mock-validated, 57/57 test lulus termasuk dengan `-race`):**

1. **`hexDecode` kustom diganti standar library.** Implementasi manual lewat `fmt.Sscanf` per-byte diganti `encoding/hex.DecodeString`, lebih sederhana, lebih cepat, dan tidak menambah surface area bug kustom untuk sesuatu yang sudah disediakan Go. Perubahan ini sempat menabrak nama fungsi lokal `hex()` (untuk parsing private key) dengan package `encoding/hex`, fungsi lokal di-rename jadi `decodePrivateKeyHex` untuk menghindari ambiguitas.

1b. **Jalur kunci privat masih memakai `fmt.Sscanf` (v16, ditemukan saat sesi uji pertama di Windows).** Klaim sebelumnya bahwa parsing hex sudah diganti ke standar library hanya berlaku untuk jalur cloid (`hexDecode`); jalur kunci privat (`decodePrivateKeyHex`) tidak ikut berubah. Temuan muncul ketika `HL_TEST_PRIVATE_KEY` diisi teks contoh dan pesan errornya hanya `expected integer`. Pemeriksaan kode menemukan dua kelemahan nyata, keduanya dibuktikan secara empiris: (a) tidak ada pengecekan panjang 32 byte, sehingga `PrivKeyFromBytes` diam-diam menghasilkan kunci LAIN untuk kunci yang terpotong; (b) `Sscanf` dengan `%02x` menerima tanda: `"+1"` menjadi byte 1 dan `"-1"` menjadi byte 255 tanpa error. Diperbaiki lewat `parsePrivateKey`: hanya `[0-9a-fA-F]`, tepat 64 karakter setelah `0x` opsional, nilai harus di rentang secp256k1 (bukan nol, bukan >= orde kurva), spasi di ujung dipangkas (`set X=val ` di cmd Windows menyimpan spasi), dan tanda kutip ditolak dengan petunjuk. Pesan error tidak pernah memuat isi kunci, hanya posisi karakter atau panjangnya (diuji: `TestParsePrivateKey_RejectsAndNeverEchoesKey`). Dry-run dengan teks contoh sekarang ditolak dengan pesan jelas, dan dengan kunci uji publik mencetak payload lengkap.

2. **Stress test concurrency untuk arsip (Temuan 3 audit sebelumnya).** Klaim desain "atomic sequence, O_EXCL, anti collision" sebelumnya hanya diuji untuk pemanggilan berurutan. Ditambahkan `TestArchiveResponseTo_ConcurrentCallsProduceUniqueFiles`: 1000 goroutine paralel memanggil `archiveResponseTo` dengan `action` dan `sentAt` yang SAMA PERSIS, memverifikasi seluruh nama file unik, tidak ada error, dan isi tiap file benar-benar milik goroutine-nya sendiri (bukan tertimpa). Lulus bersih dengan `go test -race`, tidak ada data race maupun collision.

   **Temuan lanjutan dari mesin Windows (v16):** saat dijalankan di Windows, test ini gagal dengan `connectex: No connection could be made because the target machine actively refused it`, sementara 2074 dari 2075 test lain lulus. Akar masalahnya ada di test harness, bukan di `archiveResponseTo`: error muncul saat goroutine mencoba memperoleh `*http.Response`, sebelum `archiveResponseTo` pernah dipanggil. 1000 goroutine membuka koneksi TCP ke `127.0.0.1` nyaris bersamaan, dan Windows menolak sebagian koneksi.

   **Koreksi atas perbaikan pertama:** percobaan pertama menambahkan listener manual lewat `net.Listen`, `MaxIdleConnsPerHost` tinggi, dan `dialWithRetry`, dengan komentar bahwa listener manual memberi backlog besar. Itu klaim yang tidak didukung kode: `net.Listen` tidak memberi kontrol atas backlog (ditentukan runtime Go, syscall listen, dan OS), dan listener manual itu setara dengan yang sudah dipakai `httptest` secara default, sehingga tidak mengubah apa pun. Retry dan pemakaian ulang koneksi hanya menambal gejala. Perbaikan yang benar menghilangkan akar masalahnya: test ini tidak butuh jaringan.

   **Perbaikan final:** test dibuat hermetik. Tidak ada server dan tidak ada koneksi TCP; `*http.Response` dibuat sintetis, dan 1000 goroutine dilepas serentak lewat barrier (channel yang ditutup) sehingga kontensi pada `nextArchiveSeq()` dan `O_EXCL` maksimal. Lulus 20 kali berturut-turut dengan `-race` di sandbox Linux. **Diverifikasi di Windows asli (Windows 10.0.26200, Go terpasang lokal):** `go test -count=1 ./...` lulus penuh (`hlsign` 2,2 detik, `hlsign/cmd/send` 22,6 detik). Durasi `cmd/send` jauh lebih lama daripada di Linux (sekitar 1,2 detik); dugaan penyebabnya adalah penulisan 1000 file kecil yang lebih lambat di Windows, belum dibuktikan, dan tidak memengaruhi hasil lulus. Tiga test arsip lain masih memakai `httptest.NewServer`, tapi hanya satu atau dua request berurutan dan sudah terbukti lulus di Windows.

   **Penguatan test (v16):** verifikasi isi arsip sebelumnya memakai pencarian substring (`"n": 0`), yang rapuh terhadap format dan persis sumber false failure pertama. Diganti dengan mem-parse JSON (`request_body.n` dibandingkan dengan indeks goroutine), sehingga hanya bergantung pada struktur, bukan spasi, urutan field, atau indentasi. **Uji sensitivitas (mutation check):** nomor urut atomik sengaja dirusak (selalu mengembalikan 1) dan test terbukti GAGAL; dengan kode asli ia lulus. Ini memastikan test benar-benar mendeteksi hilangnya proteksi anti-collision, bukan sekadar lulus. Lulus 20 kali berturut-turut dengan `-race`.

   **Catatan jujur dari proses:** percobaan pertama test ini gagal dengan pesan yang terlihat seperti bug konkurensi nyata (isi file "tertimpa goroutine lain"). Investigasi lewat debug test terpisah di N kecil (5 goroutine, lulus sempurna) lalu N besar dengan logging detail menunjukkan implementasi `archiveResponseTo` sendiri benar, 0 mismatch dan 0 duplikat. Akar masalahnya ada di test: `json.MarshalIndent` menghasilkan `"n": 0` (dengan spasi setelah titik dua), sementara pembanding di test mencari `"n":0` (tanpa spasi), sehingga false-failure murni dari format string yang salah, bukan dari kode yang diuji. Pelajaran yang sama berulang dari iterasi-iterasi sebelumnya: kegagalan test perlu diinvestigasi sebelum disimpulkan sebagai bug produksi, dan sebaliknya, lulus tidak boleh diterima tanpa memastikan test-nya sendiri benar.

**Presisi klaim v16 (komentar kode saja, tidak ada perubahan logika):**

1. **Anti-collision arsip adalah unik-per-proses, bukan unik-global.** `archiveSeqCounter` adalah `atomic.Int64` in-memory per proses; stress test 1000 goroutine paralel membuktikan keunikan di dalam satu proses, tapi tidak membuktikan keunikan lintas proses. Dua instance tool ini berjalan bersamaan menulis ke direktori arsip yang sama secara teoretis bisa menghasilkan nama identik pada `sentAt` yang sama sampai nanodetik. `O_EXCL` tetap menjamin ini tidak pernah jadi overwrite senyap, kalah jadi error eksplisit pada salah satu proses, bukan data silang. Kontrak tool ini diasumsikan single-process; multi-instance terhadap direktori arsip yang sama belum didukung penuh.

2. **`normalizeCloid` memvalidasi "format sesuai dokumentasi", bukan "terbukti diterima server".** Lolos dari fungsi ini berarti valid menurut aturan lokal RegimeX yang merujuk dokumentasi resmi Hyperliquid, bukan jaminan bahwa server akan menerimanya. Kode lain tidak boleh memperlakukan keduanya sebagai setara. Status tetap 🟡 sampai Fase 1.5 mengonfirmasi.

**Exit criteria:** keempat action diterima, state exchange berubah sesuai harapan, `VerifyOrderIntent` melaporkan nol mismatch untuk tiap action yang dikirim, seluruh pengiriman (termasuk yang gagal, kalau ada selama proses iterasi) tersimpan lengkap di `raw_responses/`, DAN keempat item di atas (cloid format, timeout, cakupan acceptance, kebijakan arsip) sudah diverifikasi/dikalibrasi dari observasi testnet nyata, bukan lagi dari asumsi desain.

---

## Phase 1.6: MoveStopLoss Correctness

**Latar belakang:** audit menemukan `MoveStopLoss` pada draf awal membuat stop loss sebagai order limit reduce-only, bukan order trigger. Untuk posisi long, limit sell di harga SL berada di bawah pasar sehingga marketable dan bisa langsung terisi, menutup posisi seketika saat dipasang, bukan saat harga benar-benar menyentuh level SL. Signing parity tidak menangkap ini karena signature valid untuk order yang salah.

**Perbaikan (diuji unit di `parity/go/stoploss.go` dan `stoploss_test.go`, 10/10 test lulus):**
- `BuildStopLossSpec` membangun order trigger (`tpsl: "sl"`, `isMarket: true`), bukan limit biasa.
- `ValidateStopPlacement` menolak trigger yang sudah terlampaui oleh mark price, dan trigger yang terlalu dekat dengan mark (jarak minimum dikonfigurasi).
- `WouldExecuteImmediately` adalah pengaman independen kedua: order apa pun (bukan hanya stop) ditolak jika model eksekusi sederhana menunjukkan order itu akan langsung terisi. Diuji untuk long dan short, termasuk kasus mendekati batas.
- Harga dan size dibulatkan lewat `RoundPriceHL`/`FloorSize` mengikuti aturan presisi Hyperliquid, mencegah regresi ke format fixed lama yang menghasilkan signature salah.
- `MoveStop` menjaga posisi tidak pernah tanpa proteksi selama transisi: jalur utama adalah modify atomik (stop lama tetap hidup bila modify gagal); jika modify tidak didukung, urutannya PASANG stop baru dulu baru CANCEL yang lama, tidak pernah sebaliknya. Diuji: jumlah stop hidup selalu tepat 1 di akhir path modify maupun fallback.
- `RatchetOnly`: stop hanya boleh bergerak ke arah yang lebih protektif, diuji menolak pelonggaran.
- Error yang hasilnya tidak jelas (timeout saat modify) TIDAK memicu fallback buta ke place-lalu-cancel; dikembalikan sebagai `ErrAmbiguous` supaya caller merekonsiliasi lewat cloid dan openOrders, bukan menebak.

**Dampak ke klasifikasi recovery (audit poin 1 dan 3), diuji di `recovery.go`/`recovery_test.go`:**
- `IsStopLossOrder` mensyaratkan `IsTrigger && TriggerTpsl == "sl"`; `IsTakeProfitOrder` memakai `tpsl == "tp"`. Keduanya tidak pernah tumpang tindih pada input yang sama.
- `RecoverPositionOrders` mengembalikan SL dan TP sebagai field terpisah per posisi (`HasStop`/`StopOid` dan `HasTakeProfit`/`TPOid`), bukan satu bucket "stop" yang bisa keliru memasukkan TP.
- `PickMostRecentStop` hanya memilih dari kandidat yang lolos `IsStopLossOrder`, sehingga TP tidak pernah terpilih sebagai proteksi meski Timestamp-nya lebih baru.
- Diuji eksplisit: posisi dengan SL dan TP aktif sekaligus menghasilkan kedua oid yang benar dan tidak tertukar; reduce-only biasa (bukan trigger) tidak diklasifikasikan sebagai SL maupun TP.

**Sisa yang belum bisa diselesaikan tanpa testnet:**
1. Nama field (`IsTrigger`, `TriggerTpsl`, `IsPositionTpsl`, dll) di `OpenOrderInfo` adalah kerangka; nama dan struktur persis di response nyata (`frontendOpenOrders` atau setara) harus dikonfirmasi di Fase 2 lalu dipetakan ke struct ini.
2. Perilaku `limit_px` pada trigger market (dipakai sebagai batas slippage worst-case) harus diverifikasi: apakah field ini benar-benar berfungsi sebagai limit price saat trigger tereksekusi sebagai market, atau diabaikan server.
3. Apakah order trigger mendukung `modify` (via `batchModify`) sama seperti limit order, atau memerlukan cancel-lalu-place. `MoveStop` menangani kedua kasus (`ErrModifyUnsupported` memicu fallback aman), tapi jalur mana yang sebenarnya berlaku baru diketahui dari observasi.

**Exit criteria:** buka posisi kecil di testnet, pasang SL lewat `MoveStop`, verifikasi order muncul sebagai trigger (bukan limit) di openOrders dan TIDAK langsung terisi meski mark price jauh dari trigger. Gerakkan harga API (harga testnet bisa disimulasikan atau ditunggu) mendekati trigger dan verifikasi order tereksekusi tepat saat level tersentuh, bukan sebelumnya.

---

## Phase 2: Schema Discovery

**Tujuan:** mengganti asumsi payload menjadi fakta terdokumentasi, tanpa membuat kontrak yang rapuh terhadap perubahan exchange.

**Prosedur:**
1. Buka posisi kecil di testnet, pasang SL, TP, dan reduce-only biasa pada symbol yang sama.
2. Dump mentah `openOrders`, `frontendOpenOrders`, `clearinghouseState`, `userFillsByTime` untuk: posisi kosong, posisi dengan SL, dengan SL+TP, pasca-modify, pasca-fill sebagian, pasca-fill penuh.
3. Verifikasi `isStopOrder()` eksklusif untuk SL, tidak pernah match TP atau reduce-only biasa.
4. Verifikasi semantik `timestamp` di `openOrders` (creation, update, atau lainnya) dengan menjalankan modify dan mengamati perubahannya.
5. Konfirmasi ulang lewat observasi bahwa modify menghasilkan oid baru.
6. Catat `time`/`serverTime` pada response berurutan, verifikasi monotonic dan hubungannya dengan wall clock lokal.

**Tugas khusus Position Identity (prioritas tertinggi):**
Pertanyaan yang harus dijawab: apakah ada pengenal posisi yang stabil selama satu siklus hidup posisi dan berbeda antar siklus (close lalu reopen dengan side, size, dan entry price identik)?
- Periksa semua field di `clearinghouseState.assetPositions[].position` dan di `userFills`.
- Hipotesis yang perlu diuji (belum terverifikasi): field pada fill yang menunjukkan ukuran posisi sebelum fill (mis. `startPosition`) bisa dipakai untuk menandai awal siklus posisi baru (bernilai 0), sehingga identitas posisi dapat diturunkan dari fill pembuka.
- Uji skenario eksplisit: buka, tutup, buka lagi dengan parameter identik, lalu cek apakah ada field yang membedakan kedua siklus.
- Keputusan wajib setelah pengujian: (a) ada pengenal bawaan, pakai itu; (b) tidak ada, tapi bisa diturunkan dari fill, pakai turunan itu; (c) tidak ada sama sekali, maka desain `PositionID` diubah dan precheck tidak boleh mengandalkannya sebagai satu-satunya pengaman reopen.

**Kontrak schema: critical vs optional:**
- **Critical fields** (harus ada, ketiadaannya adalah breaking change dan memicu fail-closed): mis. `oid`, `coin`, `side`, `sz`, `triggerPx` untuk stop, `cloid`, field yang dipakai `isStopOrder`.
- **Optional fields** (boleh muncul atau hilang, tidak memicu alarm): semua field lain, termasuk field baru yang belum dikenal.
- Parser mengabaikan field tak dikenal. Field baru tidak dianggap breaking change.

**Arsip payload mentah:** simpan semua dump asli di `raw/` (mis. `openOrders_001.json`, `clearinghouse_001.json`) di samping `schema_contract.md`. Kontrak adalah interpretasi; payload mentah adalah bukti, dan dibutuhkan saat ada pertanyaan "kenapa field X dianggap optional" berbulan-bulan kemudian.

**Exit criteria:** seluruh temuan dibekukan di `schema_contract.md` dengan pembagian critical/optional, dan setiap `TODO TESTNET` di kode (isStopOrder, timestamp semantics, PositionID) punya jawaban definitif beserta referensi dump payload.

---

## Phase 3: Recovery Matrix

| Kondisi | Expected |
|---|---|
| Posisi + stop aktif | RecoveredProtected |
| Posisi + TP aktif + SL aktif | Kedua teridentifikasi terpisah (HasStop dan HasTakeProfit keduanya true, oid tidak tertukar), TP tidak pernah dipilih sebagai proteksi |
| Posisi tanpa stop | RecoveredUnprotected + safe mode + alert |
| Stop tanpa posisi, milik kita | Dibatalkan, tercatat OrphanStopsCancelled |
| Stop tanpa posisi, bukan milik kita | Diabaikan, tercatat OrphanStopsIgnored |
| Snapshot tidak stabil | Orphan cleanup dilewati |
| Snapshot tidak fresh (ServerTime progression janggal) | Dianggap tidak stabil, counter reset |
| Dua kandidat stop, timestamp semantics belum terverifikasi | Degraded, bukan tebakan |
| Index safe_mode_history hilang atau salah | Startup gagal (fail-closed) |

**Partial failure jaringan (tambahan v2):**

| Kondisi | Expected |
|---|---|
| Create order sukses di exchange, ACK hilang (timeout) | Recovery via cloid: cek open orders lalu fill history, tidak membuat order ganda |
| Create stop sukses, ACK hilang | Reconcile menemukan stop dengan cloid milik kita, StopOID tersinkron, tidak membuat stop kedua |
| Modify sukses, response hilang | Reconcile lewat `pickMostRecentStopOrder`, oid baru terdeteksi |
| Cancel sukses di exchange, response timeout | Bot tidak menganggap order masih ada: verifikasi ke exchange sebelum retry, retry cancel pada order yang sudah tidak ada dianggap sukses (idempoten), bukan error |
| Cancel gagal di exchange, tapi response menyatakan sukses palsu (tidak diharapkan) | Reconcile berikutnya menemukan order masih ada dan mencatat inkonsistensi |
| Restart tepat setelah salah satu kondisi di atas | State akhir konsisten dengan exchange |

**Klasifikasi error cancel (wajib eksplisit di implementasi):**

| Respons | Perlakuan |
|---|---|
| Order tidak ditemukan, sudah cancel, atau sudah fill | Dianggap sukses (idempoten) |
| Rate limit, 5xx, temporarily unavailable, timeout | Status tidak diketahui: verifikasi lewat openOrders, TIDAK dianggap sukses |
| Error validasi atau signature | Gagal keras, tidak di-retry buta |

Pesan error persis untuk kelas pertama harus dikonfirmasi lewat observasi di Fase 2.

**Cancel idempotency, sekarang punya penanganan eksplisit (`parity/go/cancel_idempotency.go`), ditandai sebagai kandidat bug runtime paling realistis yang tersisa:**

Klasifikasi wajib dari tiap response cancel, dengan DEFAULT KONSERVATIF ke `CancelUnknown` untuk apa pun yang belum punya bukti dari testnet:

| Kategori | Kapan | Perlakuan |
|---|---|---|
| `CancelSucceeded` | HTTP 200, tanpa pesan error | Sukses |
| `CancelAlreadyGone` | Pesan cocok marker "sudah tidak ada" (TODO: isi dari dump nyata) | Diperlakukan sama seperti sukses (idempoten) |
| `CancelUnknown` | Timeout, error transport, 5xx, 429, atau pesan tak dikenal | TIDAK dianggap sukses maupun gagal; wajib `ReconcileCancel` (cek openOrders) sebelum keputusan apa pun |
| `CancelRejected` | Pesan cocok marker validasi/signature/permission | Gagal keras, tidak di-retry buta |

`knownAlreadyGoneMarkers` dan `knownRejectedMarkers` sengaja KOSONG sampai diisi dari pesan error asli hasil testnet, supaya sebelum itu sistem tidak pernah menebak kategori dari string yang belum pernah diverifikasi (diuji: `TestClassifyCancel_UnrecognizedErrorIsUnknownNotSuccessNotRejected`). Tujuannya langsung menjawab risiko yang diangkat: cancel yang sebenarnya sukses tapi timeout tidak boleh memicu safe-mode palsu atau recovery loop, dan `CancelUnknown` + `ReconcileCancel` adalah satu-satunya jalur yang menghasilkan keputusan, bukan asumsi dari pesan error semata.

**Perbaikan robustness v6 (offline/mock-validated, 32/32 test lulus):**
- Pencocokan marker sekarang case-insensitive (`"Order Not Found"` cocok dengan marker `"order not found"`), karena kapitalisasi pesan error exchange tidak terjamin konsisten. Normalisasi ini tidak menggantikan kebutuhan mengisi marker dari observasi nyata, hanya mencegah mismatch murni karena huruf besar/kecil.
- `ReconcileCancelWithCorrelation` menambah kemampuan membedakan dua kondisi yang sebelumnya tidak terpisahkan: oid lama benar-benar hilang (cancel sungguhan) versus oid lama hilang karena digantikan oid baru dengan cloid yang sama (modify, bukan cancel). Kondisi kedua TETAP `CancelUnknown`, bukan `CancelAlreadyGone`, karena order sebenarnya masih hidup dengan identitas berbeda. Status: kerangka siap pakai, aktivasi menunggu Fase 2 mengonfirmasi apakah korelasi oid-cloid memang diperlukan untuk cancel (bukan hanya untuk modify).

**Exit criteria:** seluruh baris lulus di testnet sungguhan.

---

## Phase 4: Crash Testing

**Titik injeksi `kill -9` (semua wajib):**
- Sebelum commit transaksi safe mode Enter/Exit
- **Setelah commit DB, sebelum `sm.active[symbol]` diupdate (window commit ke cache)**
- Sebelum dan sesudah nonce fallback persist
- Setelah signing, sebelum `post()` di `MoveStopLoss`
- Setelah `post()` sukses, sebelum `persistStopState`
- Di tengah `RecoverOpenStops` (setelah sebagian cancel orphan, sebelum selesai)

**Verifikasi khusus window commit ke cache:** setelah restart, `IsActive()` dan `IsActiveAuthoritative()` harus sepakat, dan tidak ada keputusan yang diambil dari `IsActive()` stale sebelum crash yang menyebabkan efek destruktif.

**Exit criteria:** setiap titik injeksi berakhir di state konsisten: sebelum operasi atau sesudah operasi, tidak pernah ambigu terhadap exchange.

---

## Phase 5: Soak Test (7 sampai 30 hari)

**Harus tetap nol:**
- panic
- deadlock
- lock timeout
- data corruption
- posisi unprotected yang tidak terdeteksi reconciliation

**Metrik terukur (dicatat berkala, dengan batas yang ditetapkan sebelum test dimulai):**
- penggunaan koneksi DB (puncak dan rata-rata) terhadap batas pool
- jumlah goroutine (harus stabil, tidak tumbuh monoton)
- pertumbuhan memori (RSS) sepanjang waktu, tanpa tren naik terus
- lock wait time per symbol (p50, p99, maksimum)
- frekuensi jalur fallback nonce
- frekuensi `stabilizedSnapshot` gagal stabil
- pertumbuhan baris `safe_mode_history` dan latensi `History(symbol)`
- rasio pemakaian REST fallback terhadap WebSocket pada price cache
- **durasi `RecoverOpenStops()` / reconciliation (p50, p95, p99, max), direkam tiap kali fungsi ini dijalankan** (bukan hanya saat restart; kalau ada reconciliation periodik di luar boot, itu juga dicatat). Tujuan: `safe_mode_history` dan jumlah order per symbol bertambah wajar seiring waktu, tapi durasi reconciliation bisa diam-diam naik dari puluhan milidetik ke hitungan detik seiring data bertambah, tanpa pernah memicu alarm dari metrik goroutine/memori/DB yang sudah ada. Degradasi ini sendiri adalah bentuk lain dari leak (bukan memory atau goroutine, tapi algoritmic/query), dan hanya terlihat dari deret waktu durasi, bukan dari nilai sesaat.

**Stress terpisah:** `Enter()` paralel 1000+ kali pada symbol sama dan symbol berbeda, periksa deadlock, starvation, lock leak, transaction timeout.

**Snapshot metrik permanen per jam (wajib, bukan hanya nilai akhir):** metrik "terukur" di atas direkam dan disimpan tiap jam selama seluruh durasi soak test, bukan hanya dibaca di akhir. Alasannya: leak lambat (goroutine 120 di hari 1, naik jadi 141 di hari 7) tidak pernah melewati threshold absolut tapi punya tren naik yang jelas, dan tren seperti itu hanya terlihat dari rangkaian snapshot, bukan dari satu angka akhir. Simpan sebagai deret waktu (CSV atau time-series DB), lalu plot tren sebelum menyatakan lulus.

**Exit criteria:** seluruh metrik "harus nol" tetap nol, seluruh metrik terukur berada di dalam batas yang ditetapkan sebelum test dimulai, DAN tidak ada tren pertumbuhan monoton pada deret waktu per jam untuk goroutine, memori, koneksi DB, maupun durasi reconciliation (uji dengan regresi linier sederhana pada snapshot; slope signifikan positif = gagal meski nilai akhir masih di bawah batas).

---

## Urutan Wajib

Phase 2 dan sebagian Phase 1 dapat berjalan paralel begitu koneksi testnet tersedia. Phase 3 ke atas mensyaratkan Phase 1 lulus penuh. Hasil Phase 2 untuk Position Identity dapat mengubah desain recovery, jadi harus selesai sebelum Phase 3 dijalankan.

---

## Lampiran: Temuan dari pembacaan SDK resmi terhadap kode awal RegimeX

Dibaca langsung dari `hyperliquid.utils.signing` dan `Exchange` (SDK 0.24.0) dan dikonfirmasi dengan uji mutasi. Kode awal yang ditulis sebelumnya di percakapan ini bermasalah pada poin berikut dan diganti oleh `parity/go/`:

| # | Masalah pada kode awal | Dampak | Status |
|---|---|---|---|
| 1 | Harga dan size diformat `%.5f` / `%.4f` | Wire string salah ("100.00000" bukan "100"), signature tidak cocok | Diganti `FloatToWire` (terbukti lewat mutasi) |
| 2 | `source` EIP-712 selalu "a" | Signature testnet ditolak (SDK memakai "b" untuk testnet) | Diperbaiki (terbukti lewat mutasi) |
| 3 | Encoding msgpack via reflection struct | Urutan key, lebar integer, omit-vs-null bergantung default library | Diganti encoder eksplisit, nil ditolak keras |
| 4 | Versi awal `signAction` tanpa byte vault dan expiresAfter | Hash berbeda dari SDK | Diperbaiki (`Suffix`) |
| 5 | Modify memakai type "modify" tunggal | SDK memakai `batchModify` dengan daftar `modifies` | Builder dan corpus memakai batchModify |
| 6 | **Stop loss dibuat sebagai order limit reduce-only (Gtc)** pada `MoveStopLoss` versi lanjutan | Limit reduce-only di harga SL (di bawah pasar untuk posisi long) bersifat marketable dan dapat langsung terisi, menutup posisi seketika. Stop loss harus berupa order trigger (`tpsl: "sl"`) | **Diperbaiki di Phase 1.6** (lihat bagian itu untuk detail): `MoveStopLoss` sekarang memakai order trigger (`tpsl: "sl"`, `isMarket: true`), dengan guard marketability ganda, ratchet-only, dan modify/fallback yang tidak pernah meninggalkan posisi tanpa stop. Diuji unit (offline/mock-validated), masih menunggu testnet validation seperti seluruh Phase 1.6 lainnya. |

Poin 6 adalah yang paling berbahaya dan tidak akan tertangkap parity signing (signature valid untuk order yang salah). Ia harus ditangkap oleh Fase 2 (klasifikasi order stop) dan uji fungsional di testnet.

---

## Lampiran B: Ringkasan Artefak `parity/go/`

| File | Isi |
|---|---|
| `signing.go` | Encoder msgpack eksplisit, connectionId, EIP-712 digest, ECDSA sign |
| `builders.go` | `FloatToWire`, `OrderWire`, `BuildAction` (order, batchModify, cancel, cancelByCloid) |
| `signing_test.go` | Parity terhadap golden dan random corpus, guard encoder, `FloatToWire` |
| `stoploss.go` | `BuildStopLossSpec`, `ValidateStopPlacement`, `WouldExecuteImmediately`, `MoveStop` |
| `stoploss_test.go` | Marketability, ratchet, atomic move (modify dan fallback), ambiguous-error handling |
| `recovery.go` | `IsStopLossOrder`, `IsTakeProfitOrder`, `PickMostRecentStop`, `RecoverPositionOrders` |

Semua lulus lokal: `cd parity/go && go test -count=1 -v ./...`
