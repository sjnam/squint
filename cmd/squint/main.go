// Command squint prints the example series from McIlroy's
// "Squinting at Power Series" using the squint package.
package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/sjnam/squint"
)

func printTerms(name string, F squint.PS, n int) {
	fmt.Printf("%-22s", name+":")
	for _, c := range F.Take(n) {
		fmt.Printf(" %s", c.RatString())
	}
	fmt.Println()
}

func main() {
	// Cancelling this context tears down every goroutine spawned below.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	negOne := big.NewRat(-1, 1)
	zero := big.NewRat(0, 1)

	printTerms("1/(1-x)", squint.Ones(ctx), 10)
	printTerms("d/dx 1/(1-x)", squint.Deriv(squint.Ones(ctx)), 10)

	FF := squint.Split(squint.Ones(ctx), 2)
	printTerms("1/(1-x)^2", squint.Mul(FF[0], FF[1]), 10)

	printTerms("1/(1/(1-x))", squint.Recip(squint.Ones(ctx)), 10)
	printTerms("exp(x)", squint.Exp(squint.X(ctx)), 10)

	GG := squint.Split(squint.Exp(squint.X(ctx)), 2)
	printTerms("exp(x)*exp(x)", squint.Mul(GG[0], GG[1]), 10)

	arctan := squint.Integ(zero, squint.Msubst(squint.Ones(ctx), negOne, 2))
	printTerms("arctan(x)", arctan, 10)

	// The paper's finale: tan(x) by reverting arctan(x).
	tan := squint.Rev(squint.Integ(zero, squint.Msubst(squint.Ones(ctx), negOne, 2)))
	printTerms("tan(x) = rev(arctan)", tan, 12)
}
