// Command squint prints the example series from McIlroy's
// "Squinting at Power Series" using the squint package.
package main

import (
	"context"
	"fmt"
	"math/big"
	"slices"

	"github.com/sjnam/squint"
)

func printTerms(name string, F squint.PS, n int) {
	fmt.Printf("%-22s", name+":")
	for c := range slices.Values(F.Take(n)) {
		fmt.Printf(" %s", c.RatString())
	}
	fmt.Println()
}

func main() {
	// Cancelling this context tears down every goroutine spawned below.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	one := big.NewRat(1, 1)
	negOne := big.NewRat(-1, 1)
	zero := big.NewRat(0, 1)

	printTerms("1/(1-x)", squint.Ones(ctx), 30)
	printTerms("d/dx 1/(1-x)", squint.Deriv(squint.Ones(ctx)), 30)

	FF := squint.Split(squint.Ones(ctx), 2)
	printTerms("1/(1-x)^2", squint.Mul(FF[0], FF[1]), 30)

	printTerms("1/(1/(1-x))", squint.Recip(squint.Ones(ctx)), 10)
	printTerms("exp(x)", squint.Exp(squint.X(ctx)), 30)

	GG := squint.Split(squint.Exp(squint.X(ctx)), 2)
	printTerms("exp(x)*exp(x)", squint.Mul(GG[0], GG[1]), 30)

	// sin and cos by integration feeding on itself, as in Exp:
	// cos = 1 - integ(sin), where sin = integ(cos).
	cos := squint.Fix(ctx, func(C squint.PS) squint.PS {
		return squint.Integ(one, squint.Cmul(negOne, squint.Integ(zero, C)))
	})
	CC := squint.Split(cos, 2)
	printTerms("sin(x)", squint.Integ(zero, CC[0]), 30)
	printTerms("cos(x)", CC[1], 30)

	arctan := squint.Integ(zero, squint.Msubst(squint.Ones(ctx), negOne, 2))
	printTerms("arctan(x)", arctan, 30)

	// The paper's finale: tan(x) by reverting arctan(x).
	tan := squint.Rev(squint.Integ(zero, squint.Msubst(squint.Ones(ctx), negOne, 2)))
	printTerms("tan(x) = rev(arctan)", tan, 30)

	// tan(x) a different way: solve the Riccati equation tan' = 1 + tan^2
	// with tan(0) = 0, again by integration feeding on itself.
	tan2 := squint.Fix(ctx, func(T squint.PS) squint.PS {
		TT := squint.Split(T, 2)
		return squint.Integ(zero,
			squint.Add(squint.Series(ctx, one), squint.Mul(TT[0], TT[1])))
	})
	printTerms("tan(x), tan'=1+tan^2", tan2, 30)
}
