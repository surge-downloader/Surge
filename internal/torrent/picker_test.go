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

func TestPiecePickerRemaining(t *testing.T) {
	p := NewPiecePicker(5)
	if r := p.Remaining(); r != 5 {
		t.Fatalf("expected 5 remaining, got %d", r)
	}
	p.Next()
	// In-flight pieces still count as remaining.
	if r := p.Remaining(); r != 5 {
		t.Fatalf("expected 5 remaining after Next(), got %d", r)
	}
	// Done pieces are not remaining.
	piece, _ := p.Next()
	p.Done(piece)
	if r := p.Remaining(); r != 4 {
		t.Fatalf("expected 4 remaining after Done(), got %d", r)
	}
}

func TestPiecePickerEndgameActivates(t *testing.T) {
	// 10 pieces, threshold = max(10/20, 2) = 2
	p := NewPiecePicker(10)
	if p.IsEndgame() {
		t.Fatalf("should not be in endgame at start")
	}
	// Complete all but 2 pieces.
	for i := 0; i < 8; i++ {
		piece, ok := p.Next()
		if !ok {
			t.Fatalf("expected piece at iteration %d", i)
		}
		p.Done(piece)
	}
	if !p.IsEndgame() {
		t.Fatalf("expected endgame with 2 remaining pieces")
	}
}

func TestPiecePickerEndgameDuplicates(t *testing.T) {
	// 4 pieces, threshold = 2
	p := NewPiecePicker(4)
	bf := bitfieldFromPieces(4, 0, 1, 2, 3)

	// Complete pieces 0 and 1.
	p0, _ := p.NextFromBitfield(bf)
	p.Done(p0)
	p1, _ := p.NextFromBitfield(bf)
	p.Done(p1)

	// Now 2 remaining -> endgame.
	if !p.IsEndgame() {
		t.Fatalf("expected endgame with 2 remaining")
	}

	// Pick piece via normal route (should assign one of the remaining).
	piece1, ok := p.NextFromBitfield(bf)
	if !ok {
		t.Fatalf("expected a piece from normal pick")
	}

	// Now that piece is in-flight, endgame should let us get it again.
	piece2, ok := p.NextFromBitfieldEndgame(bf)
	if !ok {
		t.Fatalf("expected endgame to return a piece")
	}
	// piece2 should be in-flight (either piece1 or the other remaining piece).
	if piece2 != piece1 {
		// The other pending piece was picked first — that's fine.
		// Pick again to get the duplicate.
		piece3, ok := p.NextFromBitfieldEndgame(bf)
		if !ok {
			t.Fatalf("expected duplicate endgame piece")
		}
		if piece3 != piece1 && piece3 != piece2 {
			t.Fatalf("endgame should return an in-flight piece, got %d", piece3)
		}
	}
}

func TestPiecePickerDoneInEndgame(t *testing.T) {
	// 4 pieces, threshold = 2. Complete 2 so 2 remain -> endgame.
	p := NewPiecePicker(4)
	bf := bitfieldFromPieces(4, 0, 1, 2, 3)

	p0, _ := p.NextFromBitfield(bf)
	p.Done(p0)
	p1, _ := p.NextFromBitfield(bf)
	p.Done(p1)

	if !p.IsEndgame() {
		t.Fatalf("expected endgame")
	}

	// Pick remaining 2 pieces normally (moves them to in-flight).
	piece1, _ := p.NextFromBitfield(bf)
	piece2, _ := p.NextFromBitfield(bf)

	// Endgame pick should return one of the in-flight pieces as a duplicate.
	dupPiece, ok := p.NextFromBitfieldEndgame(bf)
	if !ok {
		t.Fatalf("expected endgame piece")
	}
	if dupPiece != piece1 && dupPiece != piece2 {
		t.Fatalf("expected duplicate of an in-flight piece, got %d", dupPiece)
	}

	// Done() the piece from first peer — should stay done.
	p.Done(piece1)

	// After Done, calling Done again on the dup should be harmless (no-op).
	p.Done(dupPiece)

	// piece2 is still in-flight, so 1 remaining.
	if r := p.Remaining(); r != 1 {
		t.Fatalf("expected 1 remaining after Done, got %d", r)
	}
}
