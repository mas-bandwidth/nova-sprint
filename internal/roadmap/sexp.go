package roadmap

import (
	"bytes"
	"fmt"
	"strings"
)

type NodeKind int

const (
	NodeList NodeKind = iota
	NodeAtom
	NodeString
)

type Node struct {
	Kind     NodeKind
	Leading  string
	Value    string
	Raw      string
	Children []*Node
	Closing  string
}

type Document struct {
	Root     *Node
	Trailing string
}

func (n *Node) Print(b *bytes.Buffer) {
	b.WriteString(n.Leading)
	switch n.Kind {
	case NodeAtom:
		b.WriteString(n.Value)
	case NodeString:
		if n.Raw != "" {
			b.WriteString(n.Raw)
		} else {
			b.WriteString(QuoteString(n.Value))
		}
	case NodeList:
		b.WriteByte('(')
		for _, child := range n.Children {
			child.Print(b)
		}
		b.WriteString(n.Closing)
		b.WriteByte(')')
	}
}

func (doc *Document) Print() []byte {
	var b bytes.Buffer
	if doc.Root != nil {
		doc.Root.Print(&b)
	}
	b.WriteString(doc.Trailing)
	return b.Bytes()
}

func QuoteString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func skipWhitespaceAndComments(data []byte, pos int) (string, int) {
	start := pos
	for pos < len(data) {
		switch data[pos] {
		case ' ', '\t', '\r', '\n', '\f':
			pos++
		case ';':
			for pos < len(data) && data[pos] != '\n' && data[pos] != '\r' {
				pos++
			}
			if pos < len(data) && (data[pos] == '\n' || data[pos] == '\r') {
				if data[pos] == '\r' && pos+1 < len(data) && data[pos+1] == '\n' {
					pos++
				}
				pos++
			}
		default:
			return string(data[start:pos]), pos
		}
	}
	return string(data[start:pos]), pos
}

func isBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\f', '(', ')', ';', '"':
		return true
	}
	return false
}

func parseNode(data []byte, pos int) (*Node, int, error) {
	leading, nextPos := skipWhitespaceAndComments(data, pos)
	pos = nextPos
	if pos >= len(data) {
		return nil, pos, fmt.Errorf("unexpected end of input at byte %d", pos)
	}

	switch data[pos] {
	case '(':
		pos++
		node := &Node{Kind: NodeList, Leading: leading}
		for {
			closing, checkPos := skipWhitespaceAndComments(data, pos)
			if checkPos >= len(data) {
				return nil, checkPos, fmt.Errorf("unclosed '(' starting before byte %d: unexpected end of input", pos)
			}
			if data[checkPos] == ')' {
				node.Closing = closing
				pos = checkPos + 1
				return node, pos, nil
			}
			child, newPos, err := parseNode(data, pos)
			if err != nil {
				return nil, newPos, err
			}
			node.Children = append(node.Children, child)
			pos = newPos
		}
	case ')':
		return nil, pos, fmt.Errorf("unexpected ')' at byte %d", pos)
	case '"':
		start := pos
		pos++ // skip '"'
		var b strings.Builder
		for pos < len(data) {
			c := data[pos]
			switch c {
			case '"':
				pos++ // skip closing '"'
				raw := string(data[start:pos])
				return &Node{
					Kind:    NodeString,
					Leading: leading,
					Value:   b.String(),
					Raw:     raw,
				}, pos, nil
			case '\\':
				pos++
				if pos >= len(data) {
					return nil, pos, fmt.Errorf("unterminated string escape at byte %d", pos)
				}
				b.WriteByte(data[pos])
				pos++
			default:
				b.WriteByte(c)
				pos++
			}
		}
		return nil, pos, fmt.Errorf("unterminated string starting at byte %d", start)
	default:
		start := pos
		for pos < len(data) && !isBoundary(data[pos]) {
			pos++
		}
		val := string(data[start:pos])
		return &Node{
			Kind:    NodeAtom,
			Leading: leading,
			Value:   val,
		}, pos, nil
	}
}

func Parse(data []byte) (*Document, error) {
	leading, pos := skipWhitespaceAndComments(data, 0)
	if pos >= len(data) {
		return nil, fmt.Errorf("empty document: no s-expression found")
	}
	if data[pos] != '(' {
		return nil, fmt.Errorf("expected '(' at byte %d, got %q", pos, string(data[pos]))
	}

	root, pos, err := parseNode(data, 0)
	if err != nil {
		return nil, err
	}
	// root has its leading already set
	_ = leading

	trailing, pos := skipWhitespaceAndComments(data, pos)
	if pos < len(data) {
		return nil, fmt.Errorf("trailing characters after top-level s-expression at byte %d: %q", pos, string(data[pos:]))
	}

	return &Document{
		Root:     root,
		Trailing: trailing,
	}, nil
}
