// Package backfill 是补人队列：给对局空位挑候补，按评级区间筛选、先到先得。
//
// 实现口径：评级落在 [minRating, maxRating]（含边界）才可被挑中；按入队时间
// 升序（同时刻按玩家名）先到先得，最多挑 need 个并立即移出池；入队时刻距
// nowMs 达到 ttlMs 的候选过期出池，不计入 Pending 也不会被挑中；同一玩家
// 在池中重复 Join 返回 false 且不改其入队时间与评级。
//
// 规模：评级 treap 支持在任意评级区间内 O(log n) 取最早候选，时间堆负责
// O(log n) 过期清理，Join/Fill/Pending 均为 O(log n) 级别，Scanned 只累计
// 真正出池的候选，不随池大小乘调用次数增长。
package backfill

import "container/heap"

type candidate struct {
	player string
	rating int
	atMs   int
	alive  bool
}

// bucket 是同一评级的候选时间小顶堆。
type bucket []*candidate

func (b bucket) Len() int { return len(b) }
func (b bucket) Less(i, j int) bool {
	if b[i].atMs != b[j].atMs {
		return b[i].atMs < b[j].atMs
	}
	return b[i].player < b[j].player
}
func (b bucket) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b *bucket) Push(x any)   { *b = append(*b, x.(*candidate)) }
func (b *bucket) Pop() any {
	old := *b
	last := len(old) - 1
	item := old[last]
	*b = old[:last]
	return item
}

// node 是评级 treap 的节点，子树维护该评级区间内最早入队的候选。
type node struct {
	rating   int
	bucket   *bucket
	left     *node
	right    *node
	prio     uint64
	size     int
	earliest *candidate
}

func (n *node) pull() {
	if n == nil {
		return
	}
	n.size = 1
	n.earliest = n.bucketEarliest()
	if n.left != nil {
		n.size += n.left.size
		if earlier(n.left.earliest, n.earliest) {
			n.earliest = n.left.earliest
		}
	}
	if n.right != nil {
		n.size += n.right.size
		if earlier(n.right.earliest, n.earliest) {
			n.earliest = n.right.earliest
		}
	}
}

func (n *node) bucketEarliest() *candidate {
	if n.bucket == nil || len(*n.bucket) == 0 {
		return nil
	}
	return (*n.bucket)[0]
}

func earlier(a, b *candidate) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	if a.atMs != b.atMs {
		return a.atMs < b.atMs
	}
	return a.player < b.player
}

// Pool 是补人候选池。
type Pool struct {
	ttlMs int

	root  *node
	by    map[string]*candidate
	times timeHeap

	scanned int
	seed    uint64
}

type timeHeap []*candidate

func (h timeHeap) Len() int { return len(h) }
func (h timeHeap) Less(i, j int) bool {
	if h[i].atMs != h[j].atMs {
		return h[i].atMs < h[j].atMs
	}
	return h[i].player < h[j].player
}
func (h timeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *timeHeap) Push(x any)   { *h = append(*h, x.(*candidate)) }
func (h *timeHeap) Pop() any {
	old := *h
	last := len(old) - 1
	item := old[last]
	*h = old[:last]
	return item
}

// New 建候选池：ttlMs 是排队有效期。
func New(ttlMs int) *Pool {
	return &Pool{
		ttlMs: ttlMs,
		by:    make(map[string]*candidate),
		seed:  0x9e3779b97f4a7c15,
	}
}

func (p *Pool) nextPrio() uint64 {
	p.seed ^= p.seed << 13
	p.seed ^= p.seed >> 7
	p.seed ^= p.seed << 17
	if p.seed == 0 {
		p.seed = 1
	}
	return p.seed
}

// Join 让玩家进入候选池，返回是否成功。
// 同一玩家仍在池中（含尚未清理的过期候选）时返回 false，
// 且不改变其原有的入队时间与评级。
func (p *Pool) Join(player string, rating, atMs int) bool {
	if _, ok := p.by[player]; ok {
		return false
	}
	entry := &candidate{player: player, rating: rating, atMs: atMs, alive: true}
	p.by[player] = entry
	p.insertRating(entry)
	heap.Push(&p.times, entry)
	return true
}

func (p *Pool) insertRating(entry *candidate) {
	p.root = p.insert(p.root, entry)
}

func (p *Pool) insert(n *node, entry *candidate) *node {
	if n == nil {
		b := &bucket{entry}
		heap.Init(b)
		return &node{rating: entry.rating, bucket: b, prio: p.nextPrio(), size: 1, earliest: entry}
	}
	switch {
	case entry.rating < n.rating:
		n.left = p.insert(n.left, entry)
		if n.left.prio > n.prio {
			n = p.rotateRight(n)
		}
	case entry.rating > n.rating:
		n.right = p.insert(n.right, entry)
		if n.right.prio > n.prio {
			n = p.rotateLeft(n)
		}
	default:
		heap.Push(n.bucket, entry)
	}
	n.pull()
	return n
}

func (p *Pool) rotateRight(n *node) *node {
	left := n.left
	n.left = left.right
	left.right = n
	n.pull()
	left.pull()
	return left
}

func (p *Pool) rotateLeft(n *node) *node {
	right := n.right
	n.right = right.left
	right.left = n
	n.pull()
	right.pull()
	return right
}

func (p *Pool) merge(a, b *node) *node {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.prio > b.prio {
		a.right = p.merge(a.right, b)
		a.pull()
		return a
	}
	b.left = p.merge(a, b.left)
	b.pull()
	return b
}

// removeEarliest 从对应评级桶弹出最早候选；桶空时删掉评级节点。
func (p *Pool) removeEarliest(n **node, entry *candidate) {
	if (*n).rating == entry.rating {
		heap.Pop((*n).bucket)
		if (*n).bucket.Len() == 0 {
			*n = p.merge((*n).left, (*n).right)
			if *n != nil {
				(*n).pull()
			}
		} else {
			(*n).pull()
		}
		return
	}
	if entry.rating < (*n).rating {
		p.removeEarliest(&(*n).left, entry)
	} else {
		p.removeEarliest(&(*n).right, entry)
	}
	(*n).pull()
}

// earliestIn 返回评级区间 [lo, hi] 内最早入队的候选，O(log n)。
func (n *node) earliestIn(lo, hi int) *candidate {
	if n == nil {
		return nil
	}
	best := (*candidate)(nil)
	if lo <= n.rating {
		if cand := n.left.earliestIn(lo, hi); earlier(cand, best) {
			best = cand
		}
	}
	if lo <= n.rating && n.rating <= hi {
		if cand := n.bucketEarliest(); earlier(cand, best) {
			best = cand
		}
	}
	if n.rating <= hi {
		if cand := n.right.earliestIn(lo, hi); earlier(cand, best) {
			best = cand
		}
	}
	return best
}

// evict 清理入队时刻距 nowMs 达到 ttlMs 的候选。
func (p *Pool) evict(nowMs int) {
	for len(p.times) > 0 {
		head := p.times[0]
		if !head.alive {
			heap.Pop(&p.times)
			p.scanned++
			continue
		}
		if head.atMs+p.ttlMs > nowMs {
			return
		}
		heap.Pop(&p.times)
		head.alive = false
		delete(p.by, head.player)
		p.removeEarliest(&p.root, head)
		p.scanned++
	}
}

// Fill 给 need 个空位挑人，按入队时间升序返回挑中的玩家，挑中即移出池。
func (p *Pool) Fill(need, minRating, maxRating, nowMs int) []string {
	p.evict(nowMs)
	out := []string{}
	for need > 0 && p.root != nil {
		entry := p.root.earliestIn(minRating, maxRating)
		if entry == nil {
			break
		}
		out = append(out, entry.player)
		entry.alive = false
		delete(p.by, entry.player)
		p.removeEarliest(&p.root, entry)
		need--
	}
	return out
}

// Pending 返回池中还在排队（未过期）的候选人数。
func (p *Pool) Pending(nowMs int) int {
	p.evict(nowMs)
	return len(p.by)
}

// Scanned 是累计扫描（真正出池）的候选人数，用于规模观测。
func (p *Pool) Scanned() int { return p.scanned }
