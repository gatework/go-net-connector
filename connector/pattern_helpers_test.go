package connector

import (
	"context"
	"strings"

	expect "github.com/gatework/go-expect"
)

func testPattern(expression string) expect.Pattern {
	p, e := expect.Compile(expression)
	if e != nil {
		panic(e)
	}
	return p
}
func testStepOutput(s StepResult) string {
	v, e := s.Response().Output()
	if e != nil {
		panic(e)
	}
	return v
}
func testResultOutput(r Result) string {
	if v, ok := r.Config(); ok {
		return v
	}
	v, e := r.JoinedOutput()
	if e != nil {
		panic(e)
	}
	return v
}

type patternInput struct{ *strings.Reader }

func (p patternInput) Write(b []byte) (int, error) { return len(b), nil }
func (p patternInput) Close() error                { return nil }
func testMatchText(p expect.Pattern, text string) string {
	s, e := expect.NewSession(patternInput{strings.NewReader(text)})
	if e != nil {
		panic(e)
	}
	defer s.Close()
	r, e := s.Expect(context.Background(), []expect.Case{{Pattern: p}})
	if e != nil {
		return ""
	}
	return r.Match
}

func testPatternZero() expect.Pattern { return expect.Pattern{} }
