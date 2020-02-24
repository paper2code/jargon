package jargon

import (
	"bytes"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"

	"github.com/blevesearch/segment"
)

// Tokenize2 returns an 'iterator' of Tokens from a io.Reader. Call .Next() until it returns nil:
//
// The tokenizer is targeted to English text that contains tech terms, so things like C++ and .Net are handled as single units, as are #hashtags and @handles.
//
// It generally relies on Unicode definitions of 'punctuation' and 'symbol', with some exceptions.
//
// Tokenize returns all tokens (including white space), so text can be reconstructed with fidelity ("round tripped").
func Tokenize2(r io.Reader) *Tokens {
	t := newTokenizer2(r)
	return &Tokens{
		Next: t.next,
	}
}

type tokenizer2 struct {
	segmenter *segment.Segmenter
	buffer    []seg
	outgoing  *queue
}

func newTokenizer2(r io.Reader) *tokenizer2 {
	return &tokenizer2{
		segmenter: segment.NewSegmenter(r),
		outgoing:  &queue{},
	}
}

type seg struct {
	Bytes []byte
	Type  int
	Err   error
}

func (seg seg) Is(typ int) bool {
	return seg.Type == typ
}

func (t *tokenizer2) segment() seg {
	return seg{
		Bytes: t.segmenter.Bytes(),
		Type:  t.segmenter.Type(),
		Err:   t.segmenter.Err(),
	}
}

// next returns the next token. Call until it returns nil.
func (t *tokenizer2) next() (*Token, error) {
	if t.outgoing.len() > 0 {
		return t.outgoing.pop(), nil
	}
	for t.segmenter.Segment() {
		seg := t.segment()
		if err := seg.Err; err != nil {
			return nil, err
		}

		current := t.segment()

		// Something like a word or a grapheme?
		isword := current.Type != segment.None
		if isword {
			t.accept(current)

			// We continue to look for allowed trailing chars, such as F# or C++
			continue
		}

		// It can only be a rune at this point? Guard statement, in case that's wrong
		r, ok := tryRune(current.Bytes)
		if !ok {
			return nil, fmt.Errorf("should be a rune, but it's %q, this is likely a bug in the tokenizer", current)
		}

		if unicode.IsSpace(r) {
			// Always terminating

			// Queue the existing buffer
			if len(t.buffer) > 0 {
				t.outgoing.push(t.token())
			}

			// Accept the space & queue it
			t.accept(current)
			t.outgoing.push(t.token())

			// Send it back
			return t.outgoing.pop(), nil
		}

		// At this point, it's punct

		mightBeLeading := leadingPunct[r] // expressions like .Net
		if mightBeLeading {
			// Look ahead
			if t.segmenter.Segment() {
				lookahead := t.segment()
				isleading := lookahead.Is(segment.Letter) || lookahead.Is(segment.Number)
				if isleading {
					// Current bytes
					t.accept(current)
					// Lookahead bytes
					t.accept(lookahead)
					continue
				}

				// Else, consider it terminating

				// Queue the existing buffer
				if len(t.buffer) > 0 {
					t.outgoing.push(t.token())
				}

				// Current bytes
				t.accept(current)
				t.outgoing.push(t.token())

				// Lookahead bytes
				t.accept(lookahead)
				t.outgoing.push(t.token())
				continue
			}
		}

		// Trailing symbol?
		symbol := punctAsSymbol[r]
		if symbol {
			t.accept(current)
			continue
		}

		// Truly terminating punct

		// Queue the existing buffer
		if len(t.buffer) > 0 {
			t.outgoing.push(t.token())
		}

		t.accept(current)
		t.outgoing.push(t.token())

		return t.outgoing.pop(), nil
	}

	if len(t.buffer) > 0 {
		t.outgoing.push(t.token())
	}

	if t.outgoing.len() > 0 {
		return t.outgoing.pop(), nil
	}

	return nil, nil
}

func (t *tokenizer2) accept(s seg) {
	t.buffer = append(t.buffer, s)
}

func (t *tokenizer2) token() *Token {
	var b bytes.Buffer

	for _, seg := range t.buffer {
		b.Write(seg.Bytes)
	}

	// Got the bytes, can reset
	t.buffer = t.buffer[:0]

	// Determine punct / space
	r, ok := tryRune(b.Bytes())
	if ok {
		return newTokenFromRune(r)
	}

	return &Token{
		value: b.String(),
	}
}

func tryRune(b []byte) (rune, bool) {
	ok := utf8.RuneCount(b) == 1

	if ok {
		r, _ := utf8.DecodeRune(b)
		return r, true
	}

	return utf8.RuneError, false
}
