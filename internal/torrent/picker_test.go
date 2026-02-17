package torrent

import "testing"

func TestPiecePickerNextFromBitfieldPrefersRarestPiece(t *testing.T) {
	p := NewPiecePicker(4)

	// Availability:
	// piece 0 -> 1 peer
	// piece 1 -> 2 peers
	// piece 2 -> 2 peers
	p.ObserveBitfield(bitfieldFromPieces(4, 0, 1, 2))
	p.ObserveBitfield(bitfieldFromPieces(4, 1, 2))

	piece, ok := p.NextFromBitfield(bitfieldFromPieces(4, 0, 1, 2))
	if !ok {
		t.Fatalf("expected a piece")
	}
	if piece != 0 {
		t.Fatalf("expected rarest piece 0, got %d", piece)
	}
}

func TestPiecePickerObserveHaveAffectsSelection(t *testing.T) {
	p := NewPiecePicker(3)

	// piece 1 becomes more common than piece 2.
	p.ObserveHave(1)
	p.ObserveHave(1)
	p.ObserveHave(2)

	piece, ok := p.NextFromBitfield(bitfieldFromPieces(3, 1, 2))
	if !ok {
		t.Fatalf("expected a piece")
	}
	if piece != 2 {
		t.Fatalf("expected rarer piece 2, got %d", piece)
	}
}

func TestPiecePickerRequeueWithBitfield(t *testing.T) {
	p := NewPiecePicker(3)
	bf := bitfieldFromPieces(3, 2)

	piece, ok := p.NextFromBitfield(bf)
	if !ok || piece != 2 {
		t.Fatalf("expected initial piece 2, got piece=%d ok=%v", piece, ok)
	}

	if _, ok := p.NextFromBitfield(bf); ok {
		t.Fatalf("expected no remaining piece for this peer")
	}

	p.Requeue(2)
	piece, ok = p.NextFromBitfield(bf)
	if !ok || piece != 2 {
		t.Fatalf("expected requeued piece 2, got piece=%d ok=%v", piece, ok)
	}
}

func bitfieldFromPieces(totalPieces int, pieces ...int) []byte {
	if totalPieces <= 0 {
		return nil
	}
	bf := make([]byte, (totalPieces+7)/8)
	for _, piece := range pieces {
		if piece < 0 || piece >= totalPieces {
			continue
		}
		byteIndex := piece / 8
		mask := byte(1 << (7 - (piece % 8)))
		bf[byteIndex] |= mask
	}
	return bf
}
