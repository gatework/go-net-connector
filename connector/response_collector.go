package connector

import "strings"

// responseCollector keeps detached receive fragments and bounds tiny-fragment
// metadata. freeze discards the old fragments after its one exact-size join.
type responseCollector struct {
	parts []string
	small strings.Builder
	size  int
}

const responseBlock = 4096

func (c *responseCollector) WriteString(s string) {
	if len(s) == 0 {
		return
	}
	c.size += len(s)
	if len(s) >= responseBlock {
		c.flush()
		c.parts = append(c.parts, s)
		return
	}
	c.small.WriteString(s)
	if c.small.Len() >= responseBlock {
		c.flush()
	}
}
func (c *responseCollector) WriteRune(r rune) { c.WriteString(string(r)) }
func (c *responseCollector) Len() int         { return c.size }
func (c *responseCollector) String() string {
	// Small responses keep the builder's immutable view and need no fragment
	// index or join. Appending later cannot change bytes already exposed.
	if len(c.parts) == 0 {
		return c.small.String()
	}
	c.flush()
	text := strings.Join(c.parts, "")
	c.parts = nil
	c.small = strings.Builder{}
	if text != "" {
		c.parts = []string{text}
	}
	return text
}
func (c *responseCollector) flush() {
	if c.small.Len() > 0 {
		c.parts = append(c.parts, c.small.String())
		c.small = strings.Builder{}
	}
}
func (c *responseCollector) clone() responseCollector {
	c.flush()
	return responseCollector{parts: append([]string(nil), c.parts...), size: c.size}
}
