package hlsign

import (
	"errors"
	"testing"
)

func TestClassifyCancel_HTTPErrorIsUnknown(t *testing.T) {
	got := ClassifyCancelResponse(errors.New("context deadline exceeded"), 0, "")
	if got != CancelUnknown {
		t.Fatalf("timeout harus Unknown, dapat %v", got)
	}
}

func TestClassifyCancel_200IsSuccess(t *testing.T) {
	if got := ClassifyCancelResponse(nil, 200, ""); got != CancelSucceeded {
		t.Fatalf("200 tanpa error harus Succeeded, dapat %v", got)
	}
}

func TestClassifyCancel_UnrecognizedErrorIsUnknownNotSuccessNotRejected(t *testing.T) {
	// Sengaja: sebelum marker nyata diisi dari testnet, SEGALA pesan error
	// yang tidak cocok kategori sukses eksplisit harus jatuh ke Unknown,
	// bukan ditebak sebagai AlreadyGone atau Rejected.
	got := ClassifyCancelResponse(nil, 400, "some message not yet seen in testnet")
	if got != CancelUnknown {
		t.Fatalf("pesan tak dikenal harus Unknown (konservatif), dapat %v", got)
	}
}

func TestClassifyCancel_5xxAndRateLimitAreUnknown(t *testing.T) {
	if got := ClassifyCancelResponse(nil, 503, "Service Unavailable"); got != CancelUnknown {
		t.Fatalf("503 harus Unknown, dapat %v", got)
	}
	if got := ClassifyCancelResponse(nil, 429, "Too Many Requests"); got != CancelUnknown {
		t.Fatalf("429 harus Unknown, dapat %v", got)
	}
}

func TestReconcileCancel_NotInOpenOrdersIsIdempotentSuccess(t *testing.T) {
	if got := ReconcileCancel(false); got != CancelAlreadyGone {
		t.Fatalf("tidak ada di openOrders harus AlreadyGone, dapat %v", got)
	}
}

func TestReconcileCancel_StillPresentStaysUnknown(t *testing.T) {
	if got := ReconcileCancel(true); got != CancelUnknown {
		t.Fatalf("masih ada di openOrders harus tetap Unknown (boleh retry), dapat %v", got)
	}
}

// Regression guard: pastikan kategori sukses TIDAK PERNAH diputuskan dari pesan
// error kosong-tapi-ambigu semacam ini tanpa status 200 eksplisit.
func TestClassifyCancel_EmptyMessageWithNon200IsNotSuccess(t *testing.T) {
	if got := ClassifyCancelResponse(nil, 400, ""); got == CancelSucceeded {
		t.Fatal("status non-200 tidak boleh diklasifikasi Succeeded meski errMsg kosong")
	}
}

// Regresi Temuan 1: marker case-insensitive.
func TestClassifyCancel_MarkerMatchIsCaseInsensitive(t *testing.T) {
	old := knownAlreadyGoneMarkers
	knownAlreadyGoneMarkers = []string{"order not found"}
	defer func() { knownAlreadyGoneMarkers = old }()

	got := ClassifyCancelResponse(nil, 400, "Order Not Found")
	if got != CancelAlreadyGone {
		t.Fatalf("marker harus cocok tanpa peduli kapitalisasi, dapat %v", got)
	}
}

// Regresi Temuan 3: oid lama hilang tapi digantikan oid baru dengan cloid sama
// (modify, bukan cancel sukses sungguhan) TIDAK boleh dianggap AlreadyGone.
func TestReconcileCancelWithCorrelation_ReplacedByNewOidStaysUnknown(t *testing.T) {
	outcome, survivingOid := ReconcileCancelWithCorrelation(false, "cloid-A", map[string]int64{"cloid-A": 999})
	if outcome != CancelUnknown {
		t.Fatalf("oid lama hilang tapi cloid muncul dengan oid baru harus Unknown, dapat %v", outcome)
	}
	if survivingOid != 999 {
		t.Fatalf("surviving oid salah, dapat %d", survivingOid)
	}
}

func TestReconcileCancelWithCorrelation_TrulyGoneIsAlreadyGone(t *testing.T) {
	outcome, _ := ReconcileCancelWithCorrelation(false, "cloid-A", map[string]int64{})
	if outcome != CancelAlreadyGone {
		t.Fatalf("oid lama hilang dan cloid tidak muncul lagi harus AlreadyGone, dapat %v", outcome)
	}
}

func TestReconcileCancelWithCorrelation_StillPresentStaysUnknown(t *testing.T) {
	outcome, _ := ReconcileCancelWithCorrelation(true, "cloid-A", map[string]int64{"cloid-A": 111})
	if outcome != CancelUnknown {
		t.Fatalf("oid lama masih ada harus tetap Unknown, dapat %v", outcome)
	}
}
