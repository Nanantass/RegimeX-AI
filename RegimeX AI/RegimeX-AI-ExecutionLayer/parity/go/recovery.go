package hlsign

import "sort"

// OpenOrderInfo: subset field mentah yang dipakai klasifikasi. Nama field HARUS dikonfirmasi
// terhadap dump testnet nyata (Fase 2); ini kerangka yang bisa langsung diisi dari JSON asli.
type OpenOrderInfo struct {
	Oid            int64
	Coin           string
	Cloid          string
	IsTrigger      bool   // true jika order berbasis trigger (bukan limit biasa)
	TriggerTpsl    string // "sl", "tp", atau "" jika bukan trigger
	IsPositionTpsl bool   // true jika trigger ini terikat ke seluruh posisi (bukan child suatu order)
	ReduceOnly     bool
	Timestamp      int64 // semantik (creation vs update) harus dikonfirmasi di Fase 2
}

// IsStopLossOrder: HARUS bernilai true untuk stop dan HARUS bernilai false untuk take-profit
// maupun reduce-only biasa. Field TriggerTpsl=="sl" adalah pembeda utama; IsPositionTpsl dan
// ReduceOnly hanya penguat, bukan pengganti pengecekan tpsl.
func IsStopLossOrder(o OpenOrderInfo) bool {
	return o.IsTrigger && o.TriggerTpsl == "sl"
}

func IsTakeProfitOrder(o OpenOrderInfo) bool {
	return o.IsTrigger && o.TriggerTpsl == "tp"
}

// PickMostRecentStop: di antara kandidat SL untuk satu coin, pilih berdasarkan Timestamp
// terbesar. TP tidak pernah masuk kandidat, sehingga TP != SL dijaga di titik seleksi ini,
// bukan diasumsikan oleh caller.
func PickMostRecentStop(orders []OpenOrderInfo, coin string) (OpenOrderInfo, bool) {
	var cands []OpenOrderInfo
	for _, o := range orders {
		if o.Coin == coin && IsStopLossOrder(o) {
			cands = append(cands, o)
		}
	}
	if len(cands) == 0 {
		return OpenOrderInfo{}, false
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Timestamp > cands[j].Timestamp })
	return cands[0], true
}

type RecoveredPosition struct {
	Coin          string
	HasStop       bool
	StopOid       int64
	HasTakeProfit bool
	TPOid         int64
}

// RecoverPositionOrders: mengelompokkan order per posisi ke SL dan TP terpisah, sehingga
// kasus "posisi + TP aktif + SL aktif" (audit poin 3) memiliki hasil eksplisit, bukan
// tercampur dalam satu bucket "stop".
func RecoverPositionOrders(orders []OpenOrderInfo, coins []string) map[string]RecoveredPosition {
	out := map[string]RecoveredPosition{}
	for _, coin := range coins {
		rp := RecoveredPosition{Coin: coin}
		if sl, ok := PickMostRecentStop(orders, coin); ok {
			rp.HasStop, rp.StopOid = true, sl.Oid
		}
		var tpCands []OpenOrderInfo
		for _, o := range orders {
			if o.Coin == coin && IsTakeProfitOrder(o) {
				tpCands = append(tpCands, o)
			}
		}
		if len(tpCands) > 0 {
			sort.Slice(tpCands, func(i, j int) bool { return tpCands[i].Timestamp > tpCands[j].Timestamp })
			rp.HasTakeProfit, rp.TPOid = true, tpCands[0].Oid
		}
		out[coin] = rp
	}
	return out
}
