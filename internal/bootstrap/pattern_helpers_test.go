package bootstrap

import (
	expect "github.com/gatework/go-expect"
)

func testPattern(expression string) expect.Pattern {
	p, e := expect.Compile(expression)
	if e != nil {
		panic(e)
	}
	return p
}
