package squint

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// newCtx returns a context cancelled at the end of the test, so each
// test's process network is torn down.
func newCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

// checkTerms compares the first coefficients of F with the expected
// rationals, given as strings like "1", "-1/3".
func checkTerms(t *testing.T, name string, F PS, want []string) {
	t.Helper()
	for i, w := range want {
		got := F.Get().RatString()
		if got != w {
			t.Fatalf("%s: term %d = %s, want %s", name, i, got, w)
		}
	}
}

func TestDeriv(t *testing.T) {
	// d/dx 1/(1-x) = 1/(1-x)^2 = 1 + 2x + 3x^2 + ...
	checkTerms(t, "deriv(Ones)", Deriv(Ones(newCtx(t))),
		[]string{"1", "2", "3", "4", "5", "6"})
}

func TestAddXmulCmul(t *testing.T) {
	// Ones + x*(-1)*Ones = 1 (paper's first test, made honest with Split)
	FF := Split(Ones(newCtx(t)), 2)
	S := Add(FF[0], Xmul(Cmul(rat(-1, 1), FF[1])))
	checkTerms(t, "Ones-x*Ones", S, []string{"1", "0", "0", "0", "0"})
}

func TestMul(t *testing.T) {
	// 1/(1-x)^2 = 1 + 2x + 3x^2 + ...
	FF := Split(Ones(newCtx(t)), 2)
	checkTerms(t, "Ones*Ones", Mul(FF[0], FF[1]),
		[]string{"1", "2", "3", "4", "5", "6", "7", "8"})
}

func TestRecip(t *testing.T) {
	// 1/(1/(1-x)) = 1 - x
	checkTerms(t, "recip(Ones)", Recip(Ones(newCtx(t))),
		[]string{"1", "-1", "0", "0", "0", "0"})
}

func TestExp(t *testing.T) {
	// e^x = 1 + x + x^2/2 + x^3/6 + ...
	checkTerms(t, "exp(x)", Exp(X(newCtx(t))),
		[]string{"1", "1", "1/2", "1/6", "1/24", "1/120", "1/720"})
}

func TestSubst(t *testing.T) {
	// 1/(1-x) with x^2 substituted: 1/(1-x^2) = 1 + x^2 + x^4 + ...
	ctx := newCtx(t)
	xx := Series(ctx, rat(0, 1), rat(0, 1), rat(1, 1))
	checkTerms(t, "Ones(x^2)", Subst(Ones(ctx), xx),
		[]string{"1", "0", "1", "0", "1", "0", "1"})
}

func TestMsubst(t *testing.T) {
	// 1/(1-x) with -x^2 substituted: 1/(1+x^2) = 1 - x^2 + x^4 - ...
	checkTerms(t, "Ones(-x^2)", Msubst(Ones(newCtx(t)), rat(-1, 1), 2),
		[]string{"1", "0", "-1", "0", "1", "0", "-1"})
}

func TestInteg(t *testing.T) {
	// integ(1/(1+x^2), 0) = arctan(x) = x - x^3/3 + x^5/5 - ...
	A := Integ(rat(0, 1), Msubst(Ones(newCtx(t)), rat(-1, 1), 2))
	checkTerms(t, "arctan", A,
		[]string{"0", "1", "0", "-1/3", "0", "1/5", "0", "-1/7"})
}

func TestRev(t *testing.T) {
	// tan(x) by reverting arctan(x); the paper's final example.
	Tan := Rev(Integ(rat(0, 1), Msubst(Ones(newCtx(t)), rat(-1, 1), 2)))
	checkTerms(t, "tan", Tan, []string{
		"0", "1", "0", "1/3", "0", "2/15", "0", "17/315",
		"0", "62/2835", "0", "1382/155925",
	})
}

func TestRevSqrt(t *testing.T) {
	// Reverting F = x + x^2 gives R with R + R^2 = x:
	// R = (-1 + sqrt(1+4x))/2 = x - x^2 + 2x^3 - 5x^4 + 14x^5 - ...
	// (signed Catalan numbers)
	F := Series(newCtx(t), rat(0, 1), rat(1, 1), rat(1, 1))
	checkTerms(t, "rev(x+x^2)", Rev(F),
		[]string{"0", "1", "-1", "2", "-5", "14", "-42", "132"})
}

func TestManyTerms(t *testing.T) {
	// Make sure deep recursion in Mul keeps producing terms.
	FF := Split(Ones(newCtx(t)), 2)
	P := Mul(FF[0], FF[1])
	terms := P.Take(200)
	if got, want := terms[199].RatString(), "200"; got != want {
		t.Fatalf("term 199 = %s, want %s", got, want)
	}
}

func TestCancelStopsGoroutines(t *testing.T) {
	// Let goroutines from earlier tests drain before taking a baseline.
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	Tan := Rev(Integ(rat(0, 1), Msubst(Ones(ctx), rat(-1, 1), 2)))
	Tan.Take(20)

	if during := runtime.NumGoroutine(); during <= before {
		t.Fatalf("expected a running process network: before=%d during=%d",
			before, during)
	}

	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines leaked after cancel: before=%d now=%d",
		before, runtime.NumGoroutine())
}

func TestGetAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	F := Ones(ctx)
	if F.Get() == nil {
		t.Fatal("Get before cancel returned nil")
	}
	cancel()
	if v := F.Get(); v != nil {
		t.Fatalf("Get after cancel = %v, want nil", v)
	}
	if got := len(F.Take(5)); got != 0 {
		t.Fatalf("Take after cancel returned %d terms, want 0", got)
	}
}
