// Package textdiff computes line-based unified diffs of configuration files.
package textdiff

import (
	"fmt"
	"strings"
)

// Op is one line of an edit script.
type Op struct {
	Kind byte // ' ' kept, '-' removed, '+' added
	Line string
}

// Lines splits text into lines without their terminators. A final newline
// does not produce an empty trailing line.
func Lines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Diff returns the shortest edit script from a to b (Myers' O(ND) algorithm).
func Diff(a, b []string) []Op {
	n, m := len(a), len(b)
	limit := n + m
	if limit == 0 {
		return nil
	}
	offset := limit + 1
	v := make([]int, 2*limit+3)
	var trace [][]int
	for d := 0; d <= limit; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || k != d && v[offset+k-1] < v[offset+k+1] {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(a, b, trace, offset, d)
			}
		}
	}
	return nil
}

func backtrack(a, b []string, trace [][]int, offset, depth int) []Op {
	x, y := len(a), len(b)
	var ops []Op
	for d := depth; d > 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || k != d && v[offset+k-1] < v[offset+k+1] {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[offset+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			ops = append(ops, Op{' ', a[x-1]})
			x, y = x-1, y-1
		}
		if x == prevX {
			ops = append(ops, Op{'+', b[y-1]})
		} else {
			ops = append(ops, Op{'-', a[x-1]})
		}
		x, y = prevX, prevY
	}
	for x > 0 && y > 0 {
		ops = append(ops, Op{' ', a[x-1]})
		x, y = x-1, y-1
	}
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}
	return ops
}

// Stats counts added and removed lines.
func Stats(ops []Op) (added, removed int) {
	for _, op := range ops {
		switch op.Kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return added, removed
}

// Unified renders ops as a unified diff with context lines around changes.
// It returns "" when the inputs are identical.
func Unified(fromName, toName string, ops []Op, context int) string {
	changed := false
	for _, op := range ops {
		changed = changed || op.Kind != ' '
	}
	if !changed {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", fromName, toName)
	aLine, bLine := 1, 1
	for i := 0; i < len(ops); {
		if ops[i].Kind == ' ' {
			aLine, bLine, i = aLine+1, bLine+1, i+1
			continue
		}
		start := max(0, i-context)
		end := i
		for end < len(ops) {
			if ops[end].Kind != ' ' {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].Kind == ' ' {
				run++
			}
			if run == len(ops) || run-end > 2*context {
				end = min(len(ops), end+context)
				break
			}
			end = run
		}
		aStart, bStart := aLine-(i-start), bLine-(i-start)
		aCount, bCount := 0, 0
		for _, op := range ops[start:end] {
			if op.Kind != '+' {
				aCount++
			}
			if op.Kind != '-' {
				bCount++
			}
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for _, op := range ops[start:end] {
			b.WriteByte(op.Kind)
			b.WriteString(op.Line)
			b.WriteByte('\n')
		}
		for _, op := range ops[i:end] {
			if op.Kind != '+' {
				aLine++
			}
			if op.Kind != '-' {
				bLine++
			}
		}
		i = end
	}
	return b.String()
}
