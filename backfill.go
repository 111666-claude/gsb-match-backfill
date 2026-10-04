// Package backfill 是补人队列：给对局空位挑候补，按评级区间筛选、先到先得。
//
// 候选存在一棵按评级排序的 treap 里：评级落在区间内的候选在中序里是连续
// 一段，子树维护评级区间用于剪枝、维护最早入队节点用于先到先得；另有一个
// 按入队时间排序的最小堆负责过期清理，玩家名查重走哈希表。
// Join、Fill、Pending 都不整池扫描候选。
package backfill

import "container/heap"

// key 是候选在 treap 里的排序键：评级升序，同评级按入队时间，再按入队序号。
type key struct {
	rating int
	atMs   int
	seq    int
}

func lessKey(a, b key) bool {
	if a.rating != b.rating {
		return a.rating < b.rating
	}
	if a.atMs != b.atMs {
		return a.atMs < b.atMs
	}
	return a.seq < b.seq
}

// node 是 treap 节点。minR/maxR 是子树内的评级区间，用于区间剪枝；
// minAt 指向子树内入队最早的节点，用于先到先得。
type node struct {
	player string
	rating int
	atMs   int
	seq    int
	prio   uint64
	left   *node
	right  *node
	minR   int
	maxR   int
	minAt  *node
}

// earlier 返回入队更早（atMs 升序、同刻按 seq）的节点。
func earlier(a, b *node) *node {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.atMs != b.atMs {
		if a.atMs < b.atMs {
			return a
		}
		return b
	}
	if a.seq < b.seq {
		return a
	}
	return b
}

func pull(n *node) {
	n.minR, n.maxR = n.rating, n.rating
	n.minAt = n
	if n.left != nil {
		if n.left.minR < n.minR {
			n.minR = n.left.minR
		}
		if n.left.maxR > n.maxR {
			n.maxR = n.left.maxR
		}
		n.minAt = earlier(n.minAt, n.left.minAt)
	}
	if n.right != nil {
		if n.right.minR < n.minR {
			n.minR = n.right.minR
		}
		if n.right.maxR > n.maxR {
			n.maxR = n.right.maxR
		}
		n.minAt = earlier(n.minAt, n.right.minAt)
	}
}

func rotateRight(n *node) *node {
	l := n.left
	n.left = l.right
	l.right = n
	pull(n)
	pull(l)
	return l
}

func rotateLeft(n *node) *node {
	r := n.right
	n.right = r.left
	r.left = n
	pull(n)
	pull(r)
	return r
}

// splitmix64 给每个入队序号一个确定性的堆优先级，treap 形状与插入顺序无关。
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func insertNode(root, n *node) *node {
	if root == nil {
		return n
	}
	if lessKey(key{rating: n.rating, atMs: n.atMs, seq: n.seq}, key{rating: root.rating, atMs: root.atMs, seq: root.seq}) {
		root.left = insertNode(root.left, n)
		if root.left.prio < root.prio {
			root = rotateRight(root)
		}
	} else {
		root.right = insertNode(root.right, n)
		if root.right.prio < root.prio {
			root = rotateLeft(root)
		}
	}
	pull(root)
	return root
}

func deleteNode(root *node, k key) *node {
	if root == nil {
		return nil
	}
	rk := key{rating: root.rating, atMs: root.atMs, seq: root.seq}
	switch {
	case lessKey(k, rk):
		root.left = deleteNode(root.left, k)
	case lessKey(rk, k):
		root.right = deleteNode(root.right, k)
	default:
		if root.left == nil {
			return root.right
		}
		if root.right == nil {
			return root.left
		}
		if root.left.prio < root.right.prio {
			root = rotateRight(root)
			root.right = deleteNode(root.right, k)
		} else {
			root = rotateLeft(root)
			root.left = deleteNode(root.left, k)
		}
	}
	pull(root)
	return root
}

// expItem 是过期堆里的记录；被挑走或重新入队后旧记录变脏，弹出时跳过。
type expItem struct {
	player string
	atMs   int
	seq    int
}

type expHeap []expItem

func (h expHeap) Len() int { return len(h) }

func (h expHeap) Less(i, j int) bool {
	return h[i].atMs < h[j].atMs || (h[i].atMs == h[j].atMs && h[i].seq < h[j].seq)
}

func (h expHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *expHeap) Push(x any) { *h = append(*h, x.(expItem)) }

func (h *expHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	*h = old[:len(old)-1]
	return item
}

// Pool 是补人候选池。
type Pool struct {
	ttlMs   int
	root    *node
	members map[string]key
	expiry  expHeap
	live    int
	seq     int
	scanned int
}

// New 建候选池：ttlMs 是排队有效期。
func New(ttlMs int) *Pool {
	return &Pool{ttlMs: ttlMs, members: map[string]key{}}
}

// Join 让玩家进入候选池，返回是否成功。同一玩家已在池中则返回 false，
// 不改变他的入队时间与评级。
func (p *Pool) Join(player string, rating, atMs int) bool {
	if _, ok := p.members[player]; ok {
		return false
	}
	p.seq++
	n := &node{player: player, rating: rating, atMs: atMs, seq: p.seq, prio: splitmix64(uint64(p.seq))}
	n.minR, n.maxR = rating, rating
	n.minAt = n
	p.root = insertNode(p.root, n)
	p.members[player] = key{rating: rating, atMs: atMs, seq: p.seq}
	heap.Push(&p.expiry, expItem{player: player, atMs: atMs, seq: p.seq})
	p.live++
	return true
}

// expire 把入队时刻距 nowMs 达到 ttlMs 的候选移出池，每个候选只清理一次。
func (p *Pool) expire(nowMs int) {
	for len(p.expiry) > 0 {
		top := p.expiry[0]
		if nowMs-top.atMs < p.ttlMs {
			return
		}
		heap.Pop(&p.expiry)
		cur, ok := p.members[top.player]
		if !ok || cur.seq != top.seq || cur.atMs != top.atMs {
			continue
		}
		delete(p.members, top.player)
		p.root = deleteNode(p.root, cur)
		p.live--
		p.scanned++
	}
}

// earliest 找评级落在 [minRating, maxRating] 内入队最早的节点：
// 子树评级区间与目标不相交就整棵跳过，整棵落在区间内就直接取子树
// 最早节点，都不整池扫描。
func (p *Pool) earliest(n *node, minRating, maxRating int) *node {
	if n == nil {
		return nil
	}
	p.scanned++
	if n.maxR < minRating || n.minR > maxRating {
		return nil
	}
	if n.minR >= minRating && n.maxR <= maxRating {
		return n.minAt
	}
	best := p.earliest(n.left, minRating, maxRating)
	if n.rating >= minRating && n.rating <= maxRating {
		best = earlier(best, n)
	}
	return earlier(best, p.earliest(n.right, minRating, maxRating))
}

// Fill 给 need 个空位挑人：只看评级落在 [minRating, maxRating] 内且未过期的
// 候选，按入队时间升序最多挑 need 个，挑中的从池中移除。
func (p *Pool) Fill(need, minRating, maxRating, nowMs int) []string {
	p.expire(nowMs)
	var out []string
	for len(out) < need {
		n := p.earliest(p.root, minRating, maxRating)
		if n == nil {
			break
		}
		out = append(out, n.player)
		delete(p.members, n.player)
		p.root = deleteNode(p.root, key{rating: n.rating, atMs: n.atMs, seq: n.seq})
		p.live--
	}
	return out
}

// Pending 返回池中还在排队的候选人数，过期的不算。
func (p *Pool) Pending(nowMs int) int {
	p.expire(nowMs)
	return p.live
}

// Scanned 是累计扫描的候选人数（规模观测）。
func (p *Pool) Scanned() int { return p.scanned }
