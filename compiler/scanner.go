package compiler

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

const (
	bom = 0xFEFF // byte order mark, only permitted as very first character
	eof = -1
)

type ErrorHandler func(position Position, msg string)

type Scanner struct {
	file *File
	dir  string
	src  []byte

	err        ErrorHandler
	ErrorCount int // total errors

	scanComments bool
	flags        map[string]bool // flags for condition compiler
	flagStarted  bool            // if #if is true

	char       rune
	offset     int
	readOffset int
	lineOffset int
}

// NewScanner return an initialized scanner
func NewScanner(file *File, src []byte, err ErrorHandler, scanComment bool, flags []string) *Scanner {
	scanner := &Scanner{}

	//if file.size != len(src) {
	//panic(fmt.Sprintf("file size (%d) does not match src len (%d)", file.size, len(src)))
	//}
	scanner.file = file
	scanner.src = src
	scanner.err = err
	scanner.scanComments = scanComment
	//scanner.dir, _ = filepath.Split(file.name)

	scanner.char = ' '
	scanner.offset = 0
	scanner.readOffset = 0
	scanner.ErrorCount = 0

	scanner.next()
	if scanner.char == bom {
		scanner.next()
	}

	scanner.flags = make(map[string]bool)
	for _, flag := range flags {
		scanner.flags[flag] = true
	}

	return scanner
}

func (s *Scanner) next() {
	if s.readOffset < len(s.src) {
		s.offset = s.readOffset
		if s.char == '\n' {
			//s.file.AddLine(s.offset)
		}
		r, w := rune(s.src[s.readOffset]), 1
		switch {
		case r == 0:
			s.error(s.offset, "illegal character NUL")
		case r >= utf8.RuneSelf:
			// not ASCII
			r, w = utf8.DecodeRune(s.src[s.readOffset:])
			if r == utf8.RuneError && w == 1 {
				s.error(s.offset, "illegal UTF-8 encoding")
			} else if r == bom && s.offset > 0 {
				s.error(s.offset, "illegal byte order mark")
			}
		}
		s.readOffset += w
		s.char = r
	} else {
		s.offset = len(s.src)
		if s.char == '\n' {
			//s.file.AddLine(s.offset)
		}
		s.char = eof
	}
}

func (s *Scanner) peek() byte {
	if s.readOffset < len(s.src) {
		return s.src[s.readOffset]
	}
	return 0
}

func (s *Scanner) error(offset int, msg string) {
	fmt.Println("error:", msg)
	if s.err != nil {
		//s.err(s.file.Position(s.file.Pos(offset)), msg)
	}
	s.ErrorCount++
}

func (s *Scanner) scanComment() string {
	// initial '/' already consumed; s.ch == '/' || s.ch == '*'
	offset := s.offset - 1 // position of initial '/'

	if s.char == '/' {
		//-style comment
		// (the final '\n' is not considered part of the comment)
		s.next()
		for s.char != '\n' && s.char >= 0 {
			s.next()
		}
		// if we are at '\n', the position following the comment is afterwards
		if s.char == '\n' {
			//TO-DO update line info
		}
	} else {
		/*-style comment */
		terminated := false
		s.next()
		for s.char >= 0 {
			char := s.char
			s.next()
			if char == '*' && s.char == '/' {
				s.next()
				terminated = true
				break
			}
		}
		if !terminated {
			s.error(offset, "comment not terminated")
		}
	}
	return string(s.src[offset:s.offset])
}

func (s *Scanner) scanIdentifier() string {
	offset := s.offset
	for s.isLetter(s.char) || s.isDecimal(s.char) {
		s.next()
	}
	return string(s.src[offset:s.offset])
}

func (s *Scanner) scanDigits(base int) {
	for s.digitVal(s.char) < base {
		s.next()
	}
}

func (s *Scanner) scanNumber() (Token, string) {
	offset := s.offset
	token := INT

	if s.char != '.' {
		if s.char == '0' {
			s.next()
			if s.char != '.' {
				base := 10
				switch s.lower(s.char) {
				case 'x':
					base = 16
				case 'b':
					base = 2
				case 'o':
					base = 8
				default:
					s.error(offset, "invalid integer")
					token = ILLEGAL
				}
				if token != ILLEGAL {
					s.next()
					s.scanDigits(base)
					if s.offset-offset <= 2 {
						// only scanned "0x" or "0X"
						token = ILLEGAL
						s.error(offset, "illegal number")
					}
					if s.char == '.' {
						token = ILLEGAL
						s.error(offset, "invalid radix point")
					}
				}
			}
		} else {
			s.scanDigits(10)
		}
	}

	if s.char == '.' {
		offsetFraction := s.offset
		token = FLOAT
		s.next()
		s.scanDigits(10)
		if offsetFraction == s.offset-1 {
			token = ILLEGAL
			s.error(offset, "float has no digits after .")
		}
	}

	return token, string(s.src[offset:s.offset])
}

func (s *Scanner) scanEscape(quote rune) bool {
	offset := s.offset

	var n int
	var base, max uint32
	switch s.char {
	case 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', quote:
		s.next()
		return true
	case '0', '1', '2', '3', '4', '5', '6', '7':
		n, base, max = 3, 8, 255
	case 'x':
		s.next()
		n, base, max = 2, 16, 255
	case 'u':
		s.next()
		n, base, max = 4, 16, unicode.MaxRune
	case 'U':
		s.next()
		n, base, max = 8, 16, unicode.MaxRune
	default:
		msg := "unknown escape sequence"
		if s.char < 0 {
			msg = "escape sequence not terminated"
		}
		s.error(offset, msg)
		return false
	}

	var x uint32
	for n > 0 {
		d := uint32(s.digitVal(s.char))
		if d >= base {
			msg := fmt.Sprintf("illegal character %#U in escape sequence", s.char)
			if s.char < 0 {
				msg = "escape sequence not terminated"
			}
			s.error(s.offset, msg)
			return false
		}
		x = x*base + d
		s.next()
		n--
	}

	if x > max || 0xD800 <= x && x < 0xE000 {
		s.error(offset, "escape sequence is invalid Unicode code point")
		return false
	}

	return true
}

func (s *Scanner) scanString() string {
	offset := s.offset - 1

	for {
		char := s.char
		if char == '\n' || char < 0 {
			s.error(offset, "string literal not terminated")
			break
		}
		s.next()
		if char == '"' {
			break
		}
		if char == '\\' {
			s.scanEscape('"')
		}
	}

	return string(s.src[offset:s.offset])
}

func (s *Scanner) scanChar() string {
	// '\'' opening already consumed
	offset := s.offset - 1

	valid := true
	n := 0
	for {
		char := s.char
		if char == '\n' || char < 0 {
			if valid {
				s.error(offset, "rune literal not terminated")
				valid = false
			}
			break
		}
		s.next()
		if char == '\'' {
			break
		}
		n++
		if char == '\\' {
			switch s.char {
			case 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', '\'':
				s.next()
			default:
				s.error(offset, "illegal char literal")
				valid = false
			}
		}
	}

	if valid && n != 1 {
		s.error(offset, "illegal char literal")
	}

	return string(s.src[offset:s.offset])
}

func (s *Scanner) scanRawString() string {
	// '`' opening already consumed
	offset := s.offset - 1
	for {
		char := s.char
		if char < 0 {
			s.error(offset, "raw string literal not terminated")
			break
		}
		s.next()
		if char == '`' {
			break
		}
	}
	return string(s.src[offset:s.offset])
}

/*
func (s *Scanner) scanOperators(char rune) (rune, Token) {
	// TO-DO optimization later with tree, and opt info stored in scanner
	for HasToken(s.currentToken() + string(char)) {
		char = s.next()
	}
	return char, KeyToToken(s.currentToken())
}*/

/*
func (s *Scanner) scanPreprossesor() (rune, bool) {
	char := s.next()
	notOp := false
	if char == '!' {
		notOp = true
		char = s.next()
	}
	s.resetToken()
	if !s.isIdentifierRune(char, 0) {
		s.error(fmt.Sprintf("unexpected %s \n", string(char)))
	}
	char = s.scanIdentifier()
	for char == ' ' || char == '\t' || char == '\r' {
		char = s.next()
	}
	if char != '\n' {
		s.error("unexpected " + string(char))
	}
	result := false
	text := s.currentToken()
	if _, ok := s.flags[text]; ok {
		result = true
	}
	if notOp {
		result = !result
	}
	return char, result
}

func (s *Scanner) skipPreprossesor() rune {
	char, _ := s.scanUntil('#')
	char = s.next()
	if s.isIdentifierRune(char, 0) {
		s.resetToken()
		char = s.scanIdentifier()
		text := s.currentToken()
		if text != "end" {
			s.error(fmt.Sprintf("unexpected: %s" + text))
		}
	} else {
		s.error("unexpected: " + string(char))
	}
	return char
}*/

func (s *Scanner) isLetter(char rune) bool {
	return char == '_' || 'a' <= char && char <= 'z' || 'A' <= char && char <= 'Z'
}

func (s *Scanner) isDecimal(char rune) bool {
	return '0' <= char && char <= '9'
}

// returns lower-case char if char is ASCII letter
// use 0b00100000 instead 'a' - 'A' later in panda own compiler
func (s *Scanner) lower(char rune) rune {
	return ('a' - 'A') | char
}

func (s *Scanner) digitVal(char rune) int {
	switch {
	case '0' <= char && char <= '9':
		return int(char - '0')
	case 'a' <= s.lower(char) && s.lower(char) <= 'f':
		return int(s.lower(char) - 'a' + 10)
	}
	return 16 // larger than any legal digit val
}

// Scan next token
func (s *Scanner) Scan() (pos Position, token Token, literal string) {
	for s.char == ' ' || s.char == '\t' || s.char == '\n' || s.char == '\r' {
		s.next()
	}

	//pos = s.file.Pos(s.offset)

	token = ILLEGAL
	if s.isLetter(s.char) {
		literal = s.scanIdentifier()
		token = Lookup(literal)
	} else if s.isDecimal(s.char) || (s.char == '.' && s.isDecimal(rune(s.peek()))) {
		token, literal = s.scanNumber()
	} else {
		char := s.char
		s.next()
		switch char {
		case eof:
			token = EOF
			/*
				if s.conditionStarted {
					s.error("#if not terminated, expecting #end")
				}*/
		case '"':
			token = STRING
			literal = s.scanString()
		case '`':
			token = STRING
			literal = s.scanRawString()
		case '\'':
			token = CHAR
			literal = s.scanChar()
		case '.': //start with . can maybe operator
			//token, literal = s.scanOperators()
			/*
			   case '/':
			   			if s.ch == '/' || s.ch == '*' {
			   			} else {
			   				tok = s.switch2(token.QUO, token.QUO_ASSIGN)
			   			}
			*/
		case '/': // alse maybe operator /
			if s.char == '/' || s.char == '*' {
				literal = s.scanComment()
				if !s.scanComments {
					return s.Scan()
				}
				token = COMMENT
			} else {
				//token, literal = s.scanOperators()
			}
		case '@':
			if s.isLetter(s.char) {
				token = META
				literal = s.scanIdentifier()
			} else {
				s.error(s.offset, "invalid meta name")
			}
			/*
				case '#':
					//#if #end, before flag can add '!' for not operation
					//nested # is not supported
					char = s.next()
					if s.isIdentifierRune(char, 0) {
						s.resetToken()
						char = s.scanIdentifier()
						text := s.currentToken()
						if text == "if" {
							if s.conditionStarted {
								s.error("unexpected #if")
							}
							s.conditionStarted = true
						} else if text == "end" {
							if !s.conditionStarted {
								s.error("unexpected #end")
							}
							s.conditionStarted = false
						} else {
							s.error("unexpected: " + text)
						}

						if text == "if" {
							result := false
							char, result = s.scanPreprossesor()
							if !result {
								char = s.skipPreprossesor()
								char = s.next()
								s.conditionStarted = false
							}
						}

						s.char = char
						return s.Scan()
					}
					s.error("unexpected: " + string(char))*/
		default:
			/*
				if IsOperator(char) {
					char = s.next()
					char, token = s.scanOperators(char)
				} else*/{
				// invalid
				s.error(s.offset, "invalid token")
				s.next()
			}
		}
	}
	return
}
