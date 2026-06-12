// Package squint implements power series operations on data streams,
// following M. Douglas McIlroy, "Squinting at Power Series".
//
// A power series is represented as a stream of rational coefficients,
// the exponents given implicitly by ordinal position. Each operation
// runs as a goroutine; series are connected by channels.
//
// As in the paper, a series is a two-way "demand channel": a consumer
// sends a signal on req, then receives the next coefficient on dat.
// This forces lazy evaluation — no process computes ahead of demand.
//
// Every generator takes a context; derived series inherit the context
// of their inputs. Cancelling the context shuts down every goroutine
// in the network. Series from different contexts must not be mixed.
package squint

import (
	"context"
	"math/big"
)

// PS is a power series: a demand channel carrying rational coefficients.
// The context is stored in the handle because a PS denotes a running
// network of processes whose lifetime the context governs.
type PS struct {
	ctx context.Context
	req chan struct{}
	dat chan *big.Rat
}

func mkPS(ctx context.Context) PS {
	return PS{ctx: ctx, req: make(chan struct{}), dat: make(chan *big.Rat)}
}

// get returns the next coefficient of F, demanding its computation.
// ok is false when the context has been cancelled.
func (F PS) get() (v *big.Rat, ok bool) {
	select {
	case F.req <- struct{}{}:
	case <-F.ctx.Done():
		return nil, false
	}
	select {
	case v = <-F.dat:
		return v, true
	case <-F.ctx.Done():
		return nil, false
	}
}

// awaitReq waits for a demand on F.
func (F PS) awaitReq() bool {
	select {
	case <-F.req:
		return true
	case <-F.ctx.Done():
		return false
	}
}

// send delivers v to the consumer whose demand was already received.
func (F PS) send(v *big.Rat) bool {
	select {
	case F.dat <- v:
		return true
	case <-F.ctx.Done():
		return false
	}
}

// put waits for a demand on F, then delivers v.
func (F PS) put(v *big.Rat) bool {
	return F.awaitReq() && F.send(v)
}

// Get returns the next coefficient of F, or nil if the context that
// the series was built from has been cancelled.
func (F PS) Get() *big.Rat {
	v, _ := F.get()
	return v
}

// Take returns the first n coefficients of F. It returns fewer if the
// context is cancelled while taking.
func (F PS) Take(n int) []*big.Rat {
	cs := make([]*big.Rat, 0, n)
	for i := 0; i < n; i++ {
		v, ok := F.get()
		if !ok {
			break
		}
		cs = append(cs, v)
	}
	return cs
}

// rational helpers

func rat(a, b int64) *big.Rat     { return big.NewRat(a, b) }
func radd(x, y *big.Rat) *big.Rat { return new(big.Rat).Add(x, y) }
func rmul(x, y *big.Rat) *big.Rat { return new(big.Rat).Mul(x, y) }
func rneg(x *big.Rat) *big.Rat    { return new(big.Rat).Neg(x) }
func rinv(x *big.Rat) *big.Rat    { return new(big.Rat).Inv(x) }

// generators

// Series returns the power series whose first coefficients are cs,
// followed by zeros forever.
func Series(ctx context.Context, cs ...*big.Rat) PS {
	S := mkPS(ctx)
	go func() {
		for _, c := range cs {
			if !S.put(c) {
				return
			}
		}
		for S.put(rat(0, 1)) {
		}
	}()
	return S
}

// Ones returns 1 + x + x^2 + ..., the series for 1/(1-x).
func Ones(ctx context.Context) PS {
	S := mkPS(ctx)
	go func() {
		for S.put(rat(1, 1)) {
		}
	}()
	return S
}

// X returns the series for x.
func X(ctx context.Context) PS { return Series(ctx, rat(0, 1), rat(1, 1)) }

// Split returns n streams, each carrying every coefficient of F
// (consuming F). The branches may be read at different rates; values
// not yet seen by the slowest branch are queued.
func Split(F PS, n int) []PS {
	ctx := F.ctx
	outs := make([]PS, n)
	demand := make(chan int)
	for i := range outs {
		outs[i] = mkPS(ctx)
		go func(i int) {
			for {
				select {
				case <-outs[i].req:
				case <-ctx.Done():
					return
				}
				select {
				case demand <- i:
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}
	go func() {
		var buf []*big.Rat // F coefficients from index base onward
		base := 0
		pos := make([]int, n) // next index each branch will read
		var waiting []int     // branches waiting for the next term of F
		pulling := false
		pulled := make(chan *big.Rat)
		serve := func(i int) bool {
			if !outs[i].send(buf[pos[i]-base]) {
				return false
			}
			pos[i]++
			min := pos[0]
			for _, p := range pos[1:] {
				if p < min {
					min = p
				}
			}
			if min > base { // every branch has consumed the prefix
				buf = buf[min-base:]
				base = min
			}
			return true
		}
		// The next term of F is fetched asynchronously so that demands
		// on already-buffered terms keep being served meanwhile. This
		// matters when F is defined recursively in terms of its own
		// split (Exp, Recip, Rev): producing the next term demands
		// earlier terms back through this very splitter.
		for {
			select {
			case i := <-demand:
				if pos[i]-base < len(buf) {
					if !serve(i) {
						return
					}
					continue
				}
				waiting = append(waiting, i)
				if !pulling {
					pulling = true
					go func() {
						v, ok := F.get()
						if !ok {
							return
						}
						select {
						case pulled <- v:
						case <-ctx.Done():
						}
					}()
				}
			case v := <-pulled:
				pulling = false
				buf = append(buf, v)
				w := waiting
				waiting = nil
				for _, i := range w {
					if !serve(i) {
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return outs
}

// copyPS copies series I onto the demand channel S, one term per demand.
func copyPS(I, S PS) {
	for {
		if !S.awaitReq() {
			return
		}
		v, ok := I.get()
		if !ok {
			return
		}
		if !S.send(v) {
			return
		}
	}
}

// termwise operations

// Add returns F + G.
func Add(F, G PS) PS {
	S := mkPS(F.ctx)
	go func() {
		for {
			if !S.awaitReq() {
				return
			}
			f, ok := F.get()
			if !ok {
				return
			}
			g, ok := G.get()
			if !ok {
				return
			}
			if !S.send(radd(f, g)) {
				return
			}
		}
	}()
	return S
}

// Cmul returns c*F.
func Cmul(c *big.Rat, F PS) PS {
	S := mkPS(F.ctx)
	go func() {
		for {
			if !S.awaitReq() {
				return
			}
			f, ok := F.get()
			if !ok {
				return
			}
			if !S.send(rmul(c, f)) {
				return
			}
		}
	}()
	return S
}

// Sub returns F - G.
func Sub(F, G PS) PS { return Add(F, Cmul(rat(-1, 1), G)) }

// Xmul returns x*F: a one-element delay.
func Xmul(F PS) PS {
	S := mkPS(F.ctx)
	go func() {
		if !S.put(rat(0, 1)) {
			return
		}
		copyPS(F, S)
	}()
	return S
}

// Deriv returns dF/dx.
func Deriv(F PS) PS {
	D := mkPS(F.ctx)
	go func() {
		if !D.awaitReq() {
			return
		}
		if _, ok := F.get(); !ok { // discard constant term
			return
		}
		for n := int64(1); ; n++ {
			f, ok := F.get()
			if !ok {
				return
			}
			if !D.send(rmul(rat(n, 1), f)) {
				return
			}
			if !D.awaitReq() {
				return
			}
		}
	}()
	return D
}

// Integ returns the integral of F with constant of integration c.
// It produces c before demanding any input, which is what lets
// self-referential definitions such as Exp avoid deadlock.
func Integ(c *big.Rat, F PS) PS {
	I := mkPS(F.ctx)
	go func() {
		if !I.put(c) {
			return
		}
		for n := int64(1); ; n++ {
			if !I.awaitReq() {
				return
			}
			f, ok := F.get()
			if !ok {
				return
			}
			if !I.send(rmul(rat(1, n), f)) {
				return
			}
		}
	}()
	return I
}

// Mul returns the product F*G, computed from the recursion (eq. 2):
//
//	P = F0*G0 + x*(F0*Ḡ + G0*F̄ + x*F̄*Ḡ)
func Mul(F, G PS) PS {
	P := mkPS(F.ctx)
	go func() {
		if !P.awaitReq() {
			return
		}
		f, ok := F.get() // F and G now "contain" the tails
		if !ok {
			return
		}
		g, ok := G.get()
		if !ok {
			return
		}
		if !P.send(rmul(f, g)) {
			return
		}
		FF := Split(F, 2)
		GG := Split(G, 2)
		fG := Cmul(f, GG[0])
		gF := Cmul(g, FF[0])
		xFG := Xmul(Mul(FF[1], GG[1])) // here is the recursion
		for {
			if !P.awaitReq() {
				return
			}
			a, ok := fG.get()
			if !ok {
				return
			}
			b, ok := gF.get()
			if !ok {
				return
			}
			c, ok := xFG.get()
			if !ok {
				return
			}
			if !P.send(radd(radd(a, b), c)) {
				return
			}
		}
	}()
	return P
}

// Subst returns the composition F(G), where G0 must be 0 (eq. 3):
//
//	S = F0 + x*Ḡ*F̄(G)
func Subst(F, G PS) PS {
	S := mkPS(F.ctx)
	go func() {
		GG := Split(G, 2)
		if !S.awaitReq() {
			return
		}
		f, ok := F.get()
		if !ok {
			return
		}
		if !S.send(f) {
			return
		}
		if _, ok := GG[0].get(); !ok { // discard first term of G (must be 0)
			return
		}
		copyPS(Mul(GG[0], Subst(F, GG[1])), S)
	}()
	return S
}

// Msubst returns the monomial substitution F(c*x^n): each coefficient
// F_i is multiplied by c^i and followed by n-1 zeros.
func Msubst(F PS, c *big.Rat, n int) PS {
	S := mkPS(F.ctx)
	go func() {
		ci := rat(1, 1)
		for {
			if !S.awaitReq() {
				return
			}
			f, ok := F.get()
			if !ok {
				return
			}
			if !S.send(rmul(ci, f)) {
				return
			}
			ci = rmul(ci, c)
			for k := 0; k < n-1; k++ {
				if !S.put(rat(0, 1)) {
					return
				}
			}
		}
	}()
	return S
}

// Exp returns e^F, where F0 must be 0. It solves X' = X*F' by
// integration feeding on itself (the Picard iteration of the paper):
//
//	X = integ(X*F', 1)
func Exp(F PS) PS {
	X := mkPS(F.ctx)
	XX := Split(X, 2)
	go copyPS(Integ(rat(1, 1), Mul(XX[0], Deriv(F))), X)
	return XX[1]
}

// Recip returns 1/F, where F0 must be nonzero:
//
//	R = (1/F0)*(1 - x*F̄*R)
func Recip(F PS) PS {
	R := mkPS(F.ctx)
	RR := Split(R, 2)
	go func() {
		if !R.awaitReq() {
			return
		}
		f, ok := F.get() // F now contains F̄
		if !ok {
			return
		}
		r0 := rinv(f)
		if !R.send(r0) {
			return
		}
		copyPS(Cmul(rneg(r0), Mul(F, RR[0])), R)
	}()
	return RR[1]
}

// Rev returns the functional inverse R of F, i.e. F(R(x)) = x,
// where F0 must be 0 and F1 nonzero (eq. 8):
//
//	R̄ = (1/F1)*(1 - x*R̄²*F̿(R)),  R = x*R̄
//
// R appears three times on the right side, so R is split three ways
// internally, plus once more for the caller.
func Rev(F PS) PS {
	R := mkPS(F.ctx)
	RR := Split(R, 4)
	go func() {
		if !R.put(rat(0, 1)) { // R0 = 0
			return
		}
		if !R.awaitReq() {
			return
		}
		if _, ok := F.get(); !ok { // discard F0 (must be 0)
			return
		}
		v, ok := F.get() // F now contains F̿
		if !ok {
			return
		}
		f1 := rinv(v)
		if !R.send(f1) { // R1 = 1/F1
			return
		}
		W := Mul(Mul(tail(RR[0]), tail(RR[1])), Subst(F, RR[2])) // R̄²*F̿(R)
		c := rneg(f1)
		for {
			if !R.awaitReq() {
				return
			}
			w, ok := W.get()
			if !ok {
				return
			}
			if !R.send(rmul(c, w)) {
				return
			}
		}
	}()
	return RR[3]
}

// tail drops the constant term of F, yielding F̄.
func tail(F PS) PS {
	T := mkPS(F.ctx)
	go func() {
		if !T.awaitReq() {
			return
		}
		if _, ok := F.get(); !ok { // discard
			return
		}
		v, ok := F.get()
		if !ok {
			return
		}
		if !T.send(v) {
			return
		}
		copyPS(F, T)
	}()
	return T
}
