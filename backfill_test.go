package backfill

import "testing"

func TestEmptyPoolPicksNobody(t *testing.T) {
	if got := New(5000).Fill(1, 0, 9999, 0); len(got) != 0 {
		t.Fatalf("空池不该挑出人，得到 %v", got)
	}
}

func TestJoinThenFill(t *testing.T) {
	pool := New(5000)
	if !pool.Join("a", 1500, 0) {
		t.Fatal("入队应该成功")
	}
	if got := pool.Fill(1, 0, 9999, 0); len(got) != 1 || got[0] != "a" {
		t.Fatalf("池里唯一的人应该被挑中，得到 %v", got)
	}
}

func TestPendingCountsCandidates(t *testing.T) {
	pool := New(5000)
	pool.Join("a", 1500, 0)
	pool.Join("b", 1600, 0)
	if got := pool.Pending(0); got != 2 {
		t.Fatalf("池中应该有两个候选，得到 %d", got)
	}
}

func TestScannedStartsAtZero(t *testing.T) {
	if New(5000).Scanned() != 0 {
		t.Fatal("Scanned 初始应该是 0")
	}
}
