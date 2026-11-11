package connector

import (
	"strings"
	"testing"
)

func TestResponseCollectorBoundsFragmentMetadata(t *testing.T) {
	var c responseCollector
	for range 100000 {
		c.WriteString("x")
	}
	if len(c.parts) > 100000/responseBlock+1 {
		t.Fatal("unbounded metadata")
	}
	snapshot := c.clone()
	c.WriteString("tail")
	if snapshot.String() != strings.Repeat("x", 100000) || c.String() != strings.Repeat("x", 100000)+"tail" {
		t.Fatal("snapshot changed")
	}
	if len(c.parts) != 1 {
		t.Fatal("freeze retained fragments")
	}
}
