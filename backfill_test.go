package backfill

import (
	"fmt"
	"math/rand"
	"testing"
)

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

func assertPlayers(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("挑中名单不一致：得到 %v，想要 %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("挑中名单/顺序不一致：得到 %v，想要 %v", got, want)
		}
	}
}

func TestFillRespectsRatingRangeInclusive(t *testing.T) {
	pool := New(100000)
	pool.Join("low", 999, 0)
	pool.Join("min", 1000, 1)
	pool.Join("mid", 1500, 2)
	pool.Join("max", 2000, 3)
	pool.Join("high", 2001, 4)
	got := pool.Fill(10, 1000, 2000, 10)
	assertPlayers(t, got, []string{"min", "mid", "max"})
	if pending := pool.Pending(10); pending != 2 {
		t.Fatalf("区间外的两人应留在池中，得到 pending=%d", pending)
	}
	if got := pool.Fill(1, 1000, 2000, 10); len(got) != 0 {
		t.Fatal("再次挑选不应该选到区间外的人")
	}
}

func TestFillCapsAtNeedAndIsFIFO(t *testing.T) {
	pool := New(100000)
	names := []string{"a", "b", "c", "d", "e"}
	for i, name := range names {
		pool.Join(name, 1500, i)
	}
	assertPlayers(t, pool.Fill(2, 0, 9999, 10), []string{"a", "b"})
	if pending := pool.Pending(10); pending != 3 {
		t.Fatalf("挑走两个后应剩三个，得到 %d", pending)
	}
	assertPlayers(t, pool.Fill(2, 0, 9999, 10), []string{"c", "d"})
	assertPlayers(t, pool.Fill(2, 0, 9999, 10), []string{"e"})
	if pending := pool.Pending(10); pending != 0 {
		t.Fatalf("全部挑走后池子应为空，得到 %d", pending)
	}
}

func TestFillOrdersByJoinTimeNotCallOrder(t *testing.T) {
	pool := New(100000)
	pool.Join("late", 1500, 5)
	pool.Join("early", 1500, 1)
	assertPlayers(t, pool.Fill(2, 0, 9999, 10), []string{"early", "late"})
}

func TestExpiredCandidatesLeavePool(t *testing.T) {
	pool := New(5000)
	pool.Join("old", 1500, 0)
	pool.Join("fresh", 1500, 9000)
	if pending := pool.Pending(10000); pending != 1 {
		t.Fatalf("old 已过期不该计入，得到 pending=%d", pending)
	}
	assertPlayers(t, pool.Fill(10, 0, 9999, 10000), []string{"fresh"})
	if pending := pool.Pending(10000); pending != 0 {
		t.Fatalf("过期候选应已出池，得到 pending=%d", pending)
	}
	if pending := pool.Pending(0); pending != 0 {
		t.Fatalf("已出池的过期候选不应再回到池中，得到 pending=%d", pending)
	}
}

func TestExpiryBoundaryIsInclusive(t *testing.T) {
	pool := New(5000)
	pool.Join("a", 1500, 0)
	if pending := pool.Pending(4999); pending != 1 {
		t.Fatalf("距过期一刻还差 1ms 时不应出池，得到 %d", pending)
	}
	if pending := pool.Pending(5000); pending != 0 {
		t.Fatalf("入队时刻距 nowMs 达到 ttlMs 应出池，得到 %d", pending)
	}
}

func TestJoinDuplicateIsRejectedAndUntouched(t *testing.T) {
	pool := New(106)
	if !pool.Join("a", 1500, 0) {
		t.Fatal("首次入队应该成功")
	}
	if pool.Join("a", 9999, 5) {
		t.Fatal("同一玩家重复入队应返回 false")
	}
	if pending := pool.Pending(5); pending != 1 {
		t.Fatalf("重复入队不应占第二个位置，得到 pending=%d", pending)
	}
	// 评级仍是 1500：区间 [1000,2000] 应能挑中，而不是 9999。
	assertPlayers(t, pool.Fill(1, 1000, 2000, 5), []string{"a"})
	// 被挑走后不在池中，可以重新入队。
	if !pool.Join("a", 9999, 50) {
		t.Fatal("被挑走的玩家应能重新入队")
	}

	pool2 := New(106)
	pool2.Join("a", 1500, 0)
	pool2.Join("a", 1500, 5)
	if pending := pool2.Pending(110); pending != 0 {
		t.Fatalf("重复入队不应更新入队时间，得到 pending=%d", pending)
	}
}

func TestSameOperationSequenceIsReproducible(t *testing.T) {
	run := func() (out []string, pending []int) {
		pool := New(50)
		pool.Join("a", 1500, 0)
		pool.Join("b", 1600, 10)
		pool.Join("a", 1700, 20)
		out = append(out, pool.Fill(1, 1000, 2000, 30)...)
		pool.Join("c", 1550, 35)
		out = append(out, pool.Fill(3, 1000, 2000, 45)...)
		pending = append(pending, pool.Pending(50), pool.Pending(200))
		return
	}
	out1, pending1 := run()
	out2, pending2 := run()
	assertPlayers(t, out1, out2)
	if pending1[0] != pending2[0] || pending1[1] != pending2[1] {
		t.Fatalf("重复执行同一串操作结果应相同：%v/%v vs %v/%v", pending1[0], pending1[1], pending2[0], pending2[1])
	}
}

func TestPendingDoesNotScaleWithPoolSizeTimesCalls(t *testing.T) {
	pool := New(100000)
	for i := 0; i < 3000; i++ {
		pool.Join(fmt.Sprintf("p-%d", i), 1500, 0)
	}
	for i := 0; i < 3000; i++ {
		pool.Pending(0)
	}
	if scanned := pool.Scanned(); scanned > 6000 {
		t.Fatalf("Pending 重复查询不应按池大小乘调用次数扫描，得到 scanned=%d", scanned)
	}
}

func TestFillOutOfRangeDoesNotScanWholePool(t *testing.T) {
	pool := New(100000)
	for i := 0; i < 3000; i++ {
		pool.Join(fmt.Sprintf("p-%d", i), 3000, i)
	}
	for i := 0; i < 3000; i++ {
		if got := pool.Fill(1, 1000, 2000, i); len(got) != 0 {
			t.Fatalf("评级全在区间外不该挑到人，得到 %v", got)
		}
	}
	if scanned := pool.Scanned(); scanned > 6000 {
		t.Fatalf("区间外挑选不应整池扫描，得到 scanned=%d", scanned)
	}
}

func benchmarkFill(b *testing.B, total, need int) {
	rng := rand.New(rand.NewSource(1))
	pool := New(1 << 60)
	for i := 0; i < total; i++ {
		pool.Join(fmt.Sprintf("p-%d", i), rng.Intn(3000), i)
	}
	b.ResetTimer()
	clock := total
	for i := 0; i < b.N; i++ {
		clock++
		picked := pool.Fill(need, 1000, 2000, clock)
		for _, player := range picked {
			pool.Join(player, rng.Intn(3000), clock)
		}
	}
}

func BenchmarkFillOne(b *testing.B) { benchmarkFill(b, 200000, 1) }
