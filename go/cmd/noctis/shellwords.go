package main

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The queue-trust guard reads a shell command the way the shell reads it:
// quotes, escapes, $'…', line continuations, brace expansion, comments,
// here-documents and PowerShell's own quoting, stop-parsing token and array
// arguments. It does not run anything, so a name built at run time (from a
// variable, a command's output or pieces joined by the shell) stays unknown.

type shellDialect int

const (
	bashDialect shellDialect = iota
	powershellDialect
	cmdDialect
)

const (
	maxShellNesting = 64
	maxBraceWords   = 256
)

// Glob characters the shell still expands are kept in shellArgument.pattern as
// these private runes, so quoted * ? [ stay literal.
const (
	globAny = '\ue000'
	globOne = '\ue001'
	globSet = '\ue002'
)

type shellArgument struct {
	text    string
	quoted  bool
	pattern string
}

// shellReading is a command as its shell reads it: the words of every simple
// command in it (in a list, a pipeline, a subshell, a substitution or a
// here-document fed to a program that may run it) and text, the command for
// the quote-insensitive checks. deep is set when it nests too deep to read.
type shellReading struct {
	segments [][]shellArgument
	text     string
	deep     bool
}

type shellUnit struct {
	text, canon    string
	quoted, opaque bool
}

type heredocOpen struct {
	delimiter, reader string
	quoted, tabs      bool
}

const (
	roleArgument = iota
	roleTarget
	roleDelimiter
)

type shellReader struct {
	keepData bool
	segments [][]shellArgument
	deep     bool
}

type shellFrame struct {
	reader   *shellReader
	dialect  shellDialect
	src      string
	pos      int
	level    int
	inner    bool
	arith    bool
	out      strings.Builder
	words    []shellArgument
	all      []shellArgument
	units    []shellUnit
	run      strings.Builder
	running  bool
	quoted   bool
	inWord   bool
	role     int
	tabs     bool
	docs     []heredocOpen
	parens   int
	cases    int
	opening  bool
	pattern  bool
	verbatim bool
}

// readShellCommand reads command in dialect. Without keepData, the body of a
// here-document that only a reader (cat, tee, git, gh) gets, and that runs no
// substitution, is left out as data.
func readShellCommand(command string, dialect shellDialect, keepData bool) shellReading {
	reader := &shellReader{keepData: keepData}
	text := reader.script(command, dialect, 0, false)
	return shellReading{segments: reader.segments, text: text, deep: reader.deep}
}

func (r *shellReader) script(src string, dialect shellDialect, level int, inner bool) string {
	if level > maxShellNesting {
		r.deep = true
		return ""
	}
	f := &shellFrame{reader: r, dialect: dialect, src: src, level: level, inner: inner}
	f.read(0)
	return f.out.String()
}

func (f *shellFrame) read(stop byte) {
	switch f.dialect {
	case powershellDialect:
		f.powershell(stop)
	case cmdDialect:
		f.cmd()
	default:
		f.bash(stop)
	}
	f.endWord()
	f.endCommand()
}

func (f *shellFrame) child(pos int, arith bool) *shellFrame {
	return &shellFrame{reader: f.reader, dialect: f.dialect, src: f.src, pos: pos, level: f.level + 1, inner: true, arith: arith}
}

// nested reads the substitution that starts at f.pos and whose text starts
// skip bytes later, up to its closing stop, and returns its text.
func (f *shellFrame) nested(skip int, stop byte) (string, []shellArgument) {
	if f.level >= maxShellNesting {
		f.reader.deep = true
		f.pos = len(f.src)
		return "", nil
	}
	child := f.child(f.pos+skip, false)
	child.read(stop)
	f.pos = child.pos
	return child.out.String(), child.all
}

func (f *shellFrame) next(offset int) byte {
	if f.pos+offset < len(f.src) {
		return f.src[f.pos+offset]
	}
	return 0
}

func (f *shellFrame) add(text string, quoted bool) {
	f.inWord = true
	if !quoted && len(text) == 1 && strings.Contains("{},*?[", text) {
		f.flushRun()
		f.units = append(f.units, shellUnit{text: text})
		return
	}
	if f.running && f.quoted != quoted {
		f.flushRun()
	}
	f.running, f.quoted = true, quoted
	f.run.WriteString(text)
}

func (f *shellFrame) flushRun() {
	if f.running {
		f.units = append(f.units, shellUnit{text: f.run.String(), quoted: f.quoted})
		f.run.Reset()
		f.running = false
	}
}

func (f *shellFrame) opaque(start int, canon string) {
	f.flushRun()
	f.inWord = true
	end := min(f.pos, len(f.src))
	if start > end {
		start = end
	}
	f.units = append(f.units, shellUnit{text: f.src[start:end], canon: canon, opaque: true})
}

func (f *shellFrame) endWord() {
	if !f.inWord {
		return
	}
	f.flushRun()
	units, role := f.units, f.role
	f.units, f.inWord, f.role = nil, false, roleArgument
	alternatives := [][]shellUnit{units}
	if f.dialect == bashDialect && role != roleDelimiter {
		budget := maxBraceWords
		alternatives = expandBraces(units, &budget)
	}
	if f.dialect == powershellDialect && role == roleArgument && len(units) == 1 && !units[0].quoted && !units[0].opaque && psDashes.Replace(units[0].text) == "--%" {
		f.verbatim = true
		return
	}
	for i, alternative := range alternatives {
		if i > 0 {
			f.out.WriteByte(' ')
		}
		f.out.WriteString(renderUnits(alternative))
		word := argumentOf(alternative)
		switch {
		case role == roleDelimiter:
			reader := ""
			if len(f.words) > 0 {
				reader, _ = wordPrograms(f.words[0].text)
			}
			f.docs = append(f.docs, heredocOpen{delimiter: word.text, reader: reader, quoted: word.quoted, tabs: f.tabs})
		case role == roleTarget:
		case word.text != "" || word.quoted:
			f.caseWord(word)
			f.words = append(f.words, word)
			f.all = append(f.all, word)
		}
	}
}

func (f *shellFrame) endCommand() {
	if len(f.words) > 0 {
		f.reader.segments = append(f.reader.segments, f.words)
	}
	f.words, f.role = nil, roleArgument
}

// caseWord follows case … in … esac, whose patterns end in a ")" that closes
// no subshell or substitution.
func (f *shellFrame) caseWord(word shellArgument) {
	if f.dialect != bashDialect || word.quoted {
		return
	}
	switch {
	case len(f.words) == 0 && word.text == "case":
		f.cases++
		f.opening = true
	case f.opening && word.text == "in":
		f.opening, f.pattern = false, true
	case f.cases > 0 && len(f.words) == 0 && word.text == "esac":
		f.cases--
		f.pattern = false
	}
}

func (f *shellFrame) bash(stop byte) {
	for f.pos < len(f.src) {
		c := f.src[f.pos]
		if c == '\\' && f.next(1) == '\n' {
			f.pos += 2
			continue
		}
		if c == '#' && !f.inWord && !f.arith {
			// A comment runs to the end of the line and is never executed, so
			// it is dropped; a here-document or a command that follows it on a
			// later line is read as usual.
			end := strings.IndexByte(f.src[f.pos:], '\n')
			if end < 0 {
				end = len(f.src) - f.pos
			}
			f.pos += end
			continue
		}
		switch c {
		case ' ', '\t':
			f.endWord()
			f.out.WriteByte(c)
			f.pos++
		case '\n':
			f.endWord()
			f.endCommand()
			f.out.WriteByte('\n')
			f.pos++
			if !f.arith {
				f.readHeredocs()
			}
		case ';', '&', '|':
			if c == '&' && f.next(1) == '>' && !f.arith {
				f.redirection()
				continue
			}
			f.endWord()
			if f.pattern && c == '|' {
				f.out.WriteByte(c)
				f.pos++
				continue
			}
			f.endCommand()
			f.out.WriteByte(c)
			f.pos++
			if c == ';' && f.next(0) == ';' || c == ';' && f.next(0) == '&' {
				f.out.WriteByte(f.src[f.pos])
				f.pos++
				f.pattern = f.cases > 0
			}
		case '(':
			f.endWord()
			if f.pattern && len(f.words) == 0 {
				f.out.WriteByte(c)
				f.pos++
				continue
			}
			if !f.arith && f.next(1) == '(' && f.commandStart() {
				if end := matchingClose(f.src, f.pos+1, '(', ')'); end >= 0 && end+1 < len(f.src) && f.src[end+1] == ')' {
					f.arithmetic(f.pos+2, end, "((", "))")
					f.pos = end + 2
					continue
				}
			}
			f.endCommand()
			f.parens++
			f.out.WriteByte(c)
			f.pos++
		case ')':
			f.endWord()
			f.endCommand()
			switch {
			case f.pattern:
				f.pattern = false
			case f.parens > 0:
				f.parens--
			case stop == ')':
				f.pos++
				return
			}
			f.out.WriteByte(c)
			f.pos++
		case '<', '>':
			if f.next(1) == '(' && !f.arith {
				start := f.pos
				inner, _ := f.nested(2, ')')
				f.opaque(start, string(c)+"("+inner+")")
				continue
			}
			if f.arith {
				f.endWord()
				f.out.WriteByte(c)
				f.pos++
				continue
			}
			f.redirection()
		default:
			f.bashWordChar()
		}
	}
}

// commandStart tells whether a "((" here would open an arithmetic command:
// at the start of a command, or after a word that another command may follow.
func (f *shellFrame) commandStart() bool {
	if f.inWord {
		return false
	}
	if len(f.words) == 0 {
		return true
	}
	last := f.words[len(f.words)-1]
	return !last.quoted && strings.Contains(" if then else elif do while until ! time { for ", " "+last.text+" ")
}

func (f *shellFrame) arithmetic(start, end int, open, close string) {
	if f.level >= maxShellNesting {
		f.reader.deep = true
		return
	}
	child := &shellFrame{reader: f.reader, dialect: f.dialect, src: f.src[start:end], level: f.level + 1, inner: true, arith: true}
	child.read(0)
	f.out.WriteString(open + child.out.String() + close)
}

func (f *shellFrame) bashWordChar() {
	c := f.src[f.pos]
	switch c {
	case '\'':
		end := strings.IndexByte(f.src[f.pos+1:], '\'')
		if end < 0 {
			end = len(f.src) - f.pos - 1
		}
		f.add(f.src[f.pos+1:f.pos+1+end], true)
		f.pos = min(f.pos+end+2, len(f.src))
	case '"':
		f.pos++
		f.add("", true)
		f.dquoted(false)
	case '\\':
		if f.pos+1 < len(f.src) {
			f.add(f.src[f.pos+1:f.pos+2], true)
			f.pos += 2
		} else {
			f.add(`\`, false)
			f.pos++
		}
	case '`':
		f.backtick()
	case '$':
		f.dollar(false)
	case '{', '}', ',', '*', '?', '[':
		f.add(f.src[f.pos:f.pos+1], false)
		f.pos++
	default:
		end := f.pos + 1
		for end < len(f.src) && !strings.ContainsRune(" \t\n;&|()<>'\"\\`${},*?[]#", rune(f.src[end])) {
			end++
		}
		f.add(f.src[f.pos:end], false)
		f.pos = end
	}
}

func (f *shellFrame) redirection() {
	if f.inWord && len(f.units) == 0 && f.running && !f.quoted && isDigits(f.run.String()) {
		f.out.WriteString(f.run.String())
		f.run.Reset()
		f.running, f.inWord = false, false
	} else {
		f.endWord()
	}
	operator := ""
	for _, candidate := range []string{"<<<", "<<-", "&>>", "<<", "<&", "<>", ">>", ">&", ">|", "&>", "<", ">"} {
		if strings.HasPrefix(f.src[f.pos:], candidate) {
			operator = candidate
			break
		}
	}
	f.out.WriteString(operator)
	f.pos += len(operator)
	f.role = roleTarget
	if operator == "<<" || operator == "<<-" {
		f.role, f.tabs = roleDelimiter, operator == "<<-"
	}
}

func isDigits(text string) bool {
	return text != "" && strings.Trim(text, "0123456789") == ""
}

func (f *shellFrame) dquoted(heredoc bool) {
	for f.pos < len(f.src) {
		c := f.src[f.pos]
		switch {
		case c == '"' && !heredoc:
			f.pos++
			return
		case c == '\\' && f.pos+1 < len(f.src):
			next := f.src[f.pos+1]
			switch {
			case next == '\n':
			case next == '$' || next == '`' || next == '\\' || next == '"' && !heredoc:
				f.add(string(next), true)
			default:
				f.add(`\`+string(next), true)
			}
			f.pos += 2
		case c == '$':
			f.dollar(true)
		case c == '`':
			f.backtick()
		default:
			end := f.pos + 1
			for end < len(f.src) && !strings.ContainsRune("\"\\$`", rune(f.src[end])) {
				end++
			}
			f.add(f.src[f.pos:end], true)
			f.pos = end
		}
	}
}

func (f *shellFrame) dollar(inQuotes bool) {
	start := f.pos
	rest := f.src[f.pos+1:]
	switch {
	case strings.HasPrefix(rest, "(("):
		if end := matchingClose(f.src, f.pos+2, '(', ')'); end >= 0 && end+1 < len(f.src) && f.src[end+1] == ')' {
			f.arithmeticUnit(start, f.pos+3, end, end+2, "$((", "))")
			return
		}
		inner, _ := f.nested(2, ')')
		f.opaque(start, "$("+inner+")")
	case strings.HasPrefix(rest, "("):
		inner, _ := f.nested(2, ')')
		f.opaque(start, "$("+inner+")")
	case strings.HasPrefix(rest, "["):
		end := matchingClose(f.src, f.pos+1, '[', ']')
		if end < 0 {
			end = len(f.src)
		}
		f.arithmeticUnit(start, f.pos+2, end, min(end+1, len(f.src)), "$[", "]")
	case strings.HasPrefix(rest, "{"):
		f.braced(start, inQuotes)
	case !inQuotes && strings.HasPrefix(rest, "'"):
		f.ansiC()
	case !inQuotes && strings.HasPrefix(rest, `"`):
		f.pos += 2
		f.add("", true)
		f.dquoted(false)
	case rest != "" && (rest[0] == '_' || isLetter(rest[0])):
		end := 1
		for end < len(rest) && (rest[end] == '_' || isLetter(rest[end]) || rest[end] >= '0' && rest[end] <= '9') {
			end++
		}
		f.pos += 1 + end
		f.opaque(start, f.src[start:f.pos])
	case rest != "" && strings.IndexByte("0123456789@*#?$!-", rest[0]) >= 0:
		f.pos += 2
		f.opaque(start, f.src[start:f.pos])
	default:
		f.add("$", inQuotes)
		f.pos++
	}
}

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (f *shellFrame) arithmeticUnit(start, from, to, after int, open, close string) {
	child := &shellFrame{reader: f.reader, dialect: f.dialect, src: f.src[from:to], level: f.level + 1, inner: true, arith: true}
	if f.level >= maxShellNesting {
		f.reader.deep = true
	} else {
		child.read(0)
	}
	f.pos = after
	f.opaque(start, open+child.out.String()+close)
}

// braced reads ${…}; its value is built at run time, but a substitution in it
// still runs.
func (f *shellFrame) braced(start int, inQuotes bool) {
	child := f.child(f.pos+2, false)
	depth := 1
	for child.pos < len(child.src) && depth > 0 {
		c := child.src[child.pos]
		switch {
		case c == '\\' && child.pos+1 < len(child.src):
			child.add(child.src[child.pos+1:child.pos+2], true)
			child.pos += 2
		case c == '\'' && !inQuotes:
			end := strings.IndexByte(child.src[child.pos+1:], '\'')
			if end < 0 {
				end = len(child.src) - child.pos - 1
			}
			child.add(child.src[child.pos+1:child.pos+1+end], true)
			child.pos = min(child.pos+end+2, len(child.src))
		case c == '"':
			child.pos++
			child.add("", true)
			child.dquoted(false)
		case c == '$':
			child.dollar(inQuotes)
		case c == '`':
			child.backtick()
		default:
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
			}
			if depth > 0 {
				child.add(string(c), inQuotes)
			}
			child.pos++
		}
	}
	child.flushRun()
	f.pos = child.pos
	f.opaque(start, "${"+renderUnits(child.units)+"}")
}

func (f *shellFrame) backtick() {
	start := f.pos
	var body strings.Builder
	i := f.pos + 1
	for ; i < len(f.src) && f.src[i] != '`'; i++ {
		if f.src[i] == '\\' && i+1 < len(f.src) && strings.IndexByte("$`\\", f.src[i+1]) >= 0 {
			i++
		}
		body.WriteByte(f.src[i])
	}
	f.pos = min(i+1, len(f.src))
	inner := f.reader.script(body.String(), f.dialect, f.level+1, true)
	f.opaque(start, "$("+inner+")")
}

func (f *shellFrame) ansiC() {
	var text strings.Builder
	i := f.pos + 2
	for i < len(f.src) && f.src[i] != '\'' {
		if f.src[i] == '\\' && i+1 < len(f.src) {
			i = ansiEscape(f.src, i, &text)
			continue
		}
		text.WriteByte(f.src[i])
		i++
	}
	f.pos = min(i+1, len(f.src))
	f.add(text.String(), true)
}

func ansiEscape(src string, i int, text *strings.Builder) int {
	c := src[i+1]
	if simple := strings.IndexByte("abeEfnrtv\\'\"?", c); simple >= 0 {
		text.WriteByte("\a\b\x1b\x1b\f\n\r\t\v\\'\"?"[simple])
		return i + 2
	}
	switch {
	case c >= '0' && c <= '7':
		end := i + 1
		for end < len(src) && end < i+4 && src[end] >= '0' && src[end] <= '7' {
			end++
		}
		value, _ := strconv.ParseUint(src[i+1:end], 8, 32)
		text.WriteByte(byte(value))
		return end
	case c == 'x' || c == 'u' || c == 'U':
		digits := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
		end := i + 2
		for end < len(src) && end < i+2+digits && strings.IndexByte("0123456789abcdefABCDEF", src[end]) >= 0 {
			end++
		}
		if end == i+2 {
			break
		}
		value, _ := strconv.ParseUint(src[i+2:end], 16, 32)
		if c == 'x' {
			text.WriteByte(byte(value))
		} else {
			text.WriteRune(rune(value))
		}
		return end
	case c == 'c' && i+2 < len(src):
		text.WriteByte(src[i+2] & 0x1f)
		return i + 3
	}
	text.WriteByte('\\')
	text.WriteByte(c)
	return i + 2
}

// readHeredocs reads the bodies of the here-documents opened on the line that
// just ended. A body is data when only a reader gets it; otherwise it is read
// for what the shell substitutes in it and for what the program may run.
func (f *shellFrame) readHeredocs() {
	docs := f.docs
	f.docs = nil
	for _, doc := range docs {
		end, next, found := f.heredocEnd(doc)
		if !found {
			return
		}
		body := f.src[f.pos:end]
		data := !f.reader.keepData && heredocReaders[doc.reader] && (doc.quoted || !strings.Contains(body, "$(") && !strings.Contains(body, "`"))
		if !data {
			dialect := bashDialect
			switch doc.reader {
			case "pwsh", "powershell":
				dialect = powershellDialect
			case "cmd":
				dialect = cmdDialect
			}
			code := f.reader.script(body, dialect, f.level+1, false)
			if !doc.quoted {
				expanded := &shellFrame{reader: f.reader, dialect: bashDialect, src: body, level: f.level + 1, inner: true}
				expanded.dquoted(true)
				expanded.flushRun()
				code = renderUnits(expanded.units)
			}
			f.out.WriteString(code + "\n" + doc.delimiter + "\n")
		}
		f.pos = next
	}
}

// heredocEnd finds the line that ends doc, the way bash does: the delimiter
// alone on a line (after leading tabs for <<-, with line continuations joined
// when the delimiter is unquoted) or, inside $( ), the delimiter followed by
// text with the closing parenthesis, which bash reads on as code.
func (f *shellFrame) heredocEnd(doc heredocOpen) (end, next int, found bool) {
	for start := f.pos; start < len(f.src); {
		var line strings.Builder
		lineEnd := start
		joined := false
		for {
			newline := strings.IndexByte(f.src[lineEnd:], '\n')
			stop := len(f.src)
			if newline >= 0 {
				stop = lineEnd + newline
			}
			part := f.src[lineEnd:stop]
			if !doc.quoted && newline >= 0 && len(part)-len(strings.TrimRight(part, `\`))%2 == 1 {
				line.WriteString(part[:len(part)-1])
				lineEnd, joined = stop+1, true
				continue
			}
			line.WriteString(part)
			lineEnd = stop
			break
		}
		text := line.String()
		tabs := 0
		if doc.tabs {
			tabs = len(text) - len(strings.TrimLeft(text, "\t"))
		}
		text = text[tabs:]
		switch {
		case text == doc.delimiter:
			return start, min(lineEnd+1, len(f.src)), true
		case f.inner && !joined && doc.delimiter != "" && strings.HasPrefix(text, doc.delimiter) && strings.Contains(text[len(doc.delimiter):], ")"):
			return start, start + tabs + len(doc.delimiter), true
		}
		start = lineEnd + 1
	}
	return 0, 0, false
}

// matchingClose finds the close that matches the open at from, past quotes
// and escapes, or -1.
func matchingClose(src string, from int, open, close byte) int {
	depth := 0
	for i := from; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			end := strings.IndexByte(src[i+1:], c)
			if end < 0 {
				return -1
			}
			i += end + 1
		case c == open:
			depth++
		case c == close:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

var psDashes = strings.NewReplacer("–", "-", "—", "-", "―", "-")

func psSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\v' || r == '\f' || r == '\u0085' || unicode.Is(unicode.Zs, r)
}

func psNewline(r rune) bool {
	return r == '\n' || r == '\r' || r == ' ' || r == ' '
}

func psSingleQuote(r rune) bool {
	return r == '\'' || r >= '‘' && r <= '‛'
}

func psDoubleQuote(r rune) bool {
	return r == '"' || r >= '“' && r <= '„'
}

func (f *shellFrame) rune(offset int) (rune, int) {
	if f.pos+offset >= len(f.src) {
		return 0, 0
	}
	return utf8.DecodeRuneInString(f.src[f.pos+offset:])
}

func (f *shellFrame) powershell(stop byte) {
	for f.pos < len(f.src) {
		if f.verbatim {
			f.verbatim = false
			f.stopParsing()
			continue
		}
		r, size := f.rune(0)
		if r == '`' {
			if next, _ := f.rune(1); next == '\n' || next == '\r' {
				f.pos++
				if strings.HasPrefix(f.src[f.pos:], "\r\n") {
					f.pos++
				}
				f.pos++
				continue
			}
		}
		if !f.inWord {
			if r == '#' && (f.pos == 0 || strings.ContainsRune(";|&(){}", rune(f.src[f.pos-1])) || psSpace(lastRune(f.src[:f.pos])) || psNewline(lastRune(f.src[:f.pos]))) {
				end := strings.IndexAny(f.src[f.pos:], "\n\r")
				if end < 0 {
					end = len(f.src) - f.pos
				}
				f.pos += end
				continue
			}
			if f.hereString() {
				continue
			}
		}
		if strings.HasPrefix(f.src[f.pos:], "<#") {
			f.endWord()
			end := strings.Index(f.src[f.pos+2:], "#>")
			if end < 0 {
				end = len(f.src) - f.pos - 2
			}
			f.pos = min(f.pos+end+4, len(f.src))
			continue
		}
		switch {
		case psSpace(r):
			f.endWord()
			f.out.WriteByte(' ')
			f.pos += size
		case psNewline(r) || strings.ContainsRune(";|&{}", r):
			f.endWord()
			f.endCommand()
			f.out.WriteRune(r)
			f.pos += size
		case r == ',':
			f.endWord()
			f.out.WriteByte(',')
			f.pos++
		case r == '(':
			f.psGroup(1, "(")
		case r == ')':
			f.endWord()
			f.endCommand()
			if stop == ')' {
				f.pos++
				return
			}
			f.out.WriteByte(')')
			f.pos++
		case r == '<' || r == '>':
			f.psRedirection()
		default:
			f.psWordChar(r, size)
		}
	}
}

func lastRune(text string) rune {
	r, _ := utf8.DecodeLastRuneInString(text)
	return r
}

// psGroup reads (…), $(…) or @(…). Standing alone as an argument, its values
// are arguments of their own, so a native command gets each of them.
func (f *shellFrame) psGroup(skip int, open string) {
	alone := !f.inWord
	start := f.pos
	if alone {
		f.endWord()
	}
	inner, values := f.nested(skip, ')')
	if alone && (f.pos >= len(f.src) || strings.ContainsRune(" \t,;|&){}\n\r", rune(f.src[f.pos]))) {
		f.words = append(f.words, values...)
		f.all = append(f.all, values...)
		f.out.WriteString(open + inner + ")")
		return
	}
	f.opaque(start, open+inner+")")
}

func (f *shellFrame) psRedirection() {
	if f.inWord && len(f.units) == 0 && f.running && !f.quoted && (isDigits(f.run.String()) || f.run.String() == "*") {
		f.out.WriteString(f.run.String())
		f.run.Reset()
		f.running, f.inWord = false, false
	} else if f.inWord && len(f.units) == 1 && !f.running && f.units[0].text == "*" {
		f.out.WriteByte('*')
		f.units, f.inWord = nil, false
	} else {
		f.endWord()
	}
	operator := f.src[f.pos : f.pos+1]
	if f.next(1) == '>' {
		operator += ">"
	}
	if f.next(len(operator)) == '&' && f.next(len(operator)+1) >= '0' && f.next(len(operator)+1) <= '9' {
		operator = f.src[f.pos : f.pos+len(operator)+2]
		f.out.WriteString(operator)
		f.pos += len(operator)
		return
	}
	f.out.WriteString(operator)
	f.pos += len(operator)
	f.role = roleTarget
}

func (f *shellFrame) psWordChar(r rune, size int) {
	alone := !f.inWord
	switch {
	case psSingleQuote(r):
		f.pos += size
		var text strings.Builder
		for f.pos < len(f.src) {
			q, qsize := f.rune(0)
			f.pos += qsize
			if psSingleQuote(q) {
				if next, nsize := f.rune(0); psSingleQuote(next) {
					text.WriteRune(next)
					f.pos += nsize
					continue
				}
				break
			}
			text.WriteRune(q)
		}
		f.add(text.String(), true)
		if alone {
			f.endWord()
		}
	case psDoubleQuote(r):
		f.pos += size
		f.add("", true)
		f.psDoubleQuoted(false)
		if alone {
			f.endWord()
		}
	case r == '`':
		if next, nsize := f.rune(1); nsize > 0 {
			f.add(string(next), true)
			f.pos += 1 + nsize
		} else {
			f.add("`", false)
			f.pos++
		}
	case r == '$':
		f.psDollar(false)
	case r == '@' && f.next(1) == '(':
		f.psGroup(2, "$(")
	case r == '@' && f.next(1) == '{':
		f.endWord()
		f.endCommand()
		f.out.WriteString("@{")
		f.pos += 2
	default:
		end := f.pos + size
		if !strings.ContainsRune("{},*?[", r) {
			for end < len(f.src) {
				next, nsize := utf8.DecodeRuneInString(f.src[end:])
				if psSpace(next) || psNewline(next) || psSingleQuote(next) || psDoubleQuote(next) || strings.ContainsRune(";|&{}(),<>`$@*?[#", next) {
					break
				}
				end += nsize
			}
		}
		f.add(f.src[f.pos:end], false)
		f.pos = end
	}
}

func (f *shellFrame) psDollar(inQuotes bool) {
	start := f.pos
	switch next := f.next(1); {
	case next == '(':
		if inQuotes {
			inner, _ := f.nested(2, ')')
			f.opaque(start, "$("+inner+")")
			return
		}
		f.psGroup(2, "$(")
	case next == '{':
		end := strings.IndexByte(f.src[f.pos:], '}')
		if end < 0 {
			end = len(f.src) - f.pos - 1
		}
		f.pos += end + 1
		f.opaque(start, f.src[start:f.pos])
	case next == '_' || next == '?' || next == '^' || next == '$' || isLetter(next) || next >= '0' && next <= '9':
		end := f.pos + 2
		for end < len(f.src) && (f.src[end] == '_' || f.src[end] == ':' || isLetter(f.src[end]) || f.src[end] >= '0' && f.src[end] <= '9') {
			end++
		}
		f.pos = end
		f.opaque(start, f.src[start:f.pos])
	default:
		f.add("$", inQuotes)
		f.pos++
	}
}

func (f *shellFrame) psDoubleQuoted(here bool) {
	for f.pos < len(f.src) {
		r, size := f.rune(0)
		switch {
		case !here && psDoubleQuote(r):
			f.pos += size
			if next, nsize := f.rune(0); psDoubleQuote(next) {
				f.add(string(next), true)
				f.pos += nsize
				continue
			}
			return
		case r == '`':
			if next, nsize := f.rune(1); nsize > 0 {
				f.add(string(next), true)
				f.pos += 1 + nsize
				continue
			}
			f.add("`", true)
			f.pos++
		case r == '$':
			f.psDollar(true)
		default:
			f.add(string(r), true)
			f.pos += size
		}
	}
}

// hereString reads a PowerShell here-string (@'…'@ or @"…"@), whose closing
// quote and @ start a line.
func (f *shellFrame) hereString() bool {
	if f.src[f.pos] != '@' {
		return false
	}
	quote, size := f.rune(1)
	if !psSingleQuote(quote) && !psDoubleQuote(quote) {
		return false
	}
	body := f.pos + 1 + size
	for body < len(f.src) && (f.src[body] == ' ' || f.src[body] == '\t') {
		body++
	}
	switch {
	case strings.HasPrefix(f.src[body:], "\r\n"):
		body += 2
	case strings.HasPrefix(f.src[body:], "\n"):
		body++
	default:
		return false
	}
	end, next := len(f.src), len(f.src)
	for line := body; line < len(f.src); {
		closing, csize := utf8.DecodeRuneInString(f.src[line:])
		if (psSingleQuote(quote) && psSingleQuote(closing) || psDoubleQuote(quote) && psDoubleQuote(closing)) && strings.HasPrefix(f.src[line+csize:], "@") {
			end, next = max(line-1, body), line+csize+1
			if end > body && f.src[end-1] == '\r' {
				end--
			}
			break
		}
		newline := strings.IndexByte(f.src[line:], '\n')
		if newline < 0 {
			break
		}
		line += newline + 1
	}
	text := f.src[body:min(end, len(f.src))]
	if psSingleQuote(quote) {
		f.add(text, true)
	} else {
		expanded := &shellFrame{reader: f.reader, dialect: powershellDialect, src: text, level: f.level + 1, inner: true}
		expanded.add("", true)
		expanded.psDoubleQuoted(true)
		expanded.flushRun()
		f.flushRun()
		f.inWord = true
		f.units = append(f.units, expanded.units...)
	}
	f.pos = next
	f.endWord()
	return true
}

// stopParsing reads what follows --%: PowerShell hands the rest of the line
// to the program as it is written.
func (f *shellFrame) stopParsing() {
	end := strings.IndexAny(f.src[f.pos:], "\n\r|")
	if end < 0 {
		end = len(f.src) - f.pos
	}
	rest := f.src[f.pos : f.pos+end]
	f.out.WriteString(rest)
	f.pos += end
	for _, field := range strings.FieldsFunc(rest, psSpace) {
		word := shellArgument{text: strings.ReplaceAll(field, `"`, "")}
		f.words = append(f.words, word)
		f.all = append(f.all, word)
	}
}

// cmd reads a cmd.exe command line: double quotes, ^ escapes and its
// command separators; programs split their own arguments at blanks.
func (f *shellFrame) cmd() {
	quoted := false
	for f.pos < len(f.src) {
		c := f.src[f.pos]
		switch {
		case quoted:
			if c == '"' {
				quoted = false
			} else {
				f.add(string(c), true)
			}
			f.pos++
		case c == '"':
			quoted = true
			f.add("", true)
			f.pos++
		case c == '^' && f.pos+1 < len(f.src):
			if f.src[f.pos+1] != '\n' {
				f.add(f.src[f.pos+1:f.pos+2], true)
			}
			f.pos += 2
		case c == ' ' || c == '\t':
			f.endWord()
			f.out.WriteByte(c)
			f.pos++
		case strings.IndexByte("\n\r&|()", c) >= 0:
			f.endWord()
			f.endCommand()
			f.out.WriteByte(c)
			f.pos++
		case c == '<' || c == '>':
			f.endWord()
			f.out.WriteByte(c)
			f.pos++
			f.role = roleTarget
		default:
			f.add(string(c), false)
			f.pos++
		}
	}
}

func argumentOf(units []shellUnit) shellArgument {
	var text, pattern strings.Builder
	word := shellArgument{}
	glob := false
	for _, unit := range units {
		text.WriteString(unit.text)
		word.quoted = word.quoted || unit.quoted
		if !unit.quoted && !unit.opaque && len(unit.text) == 1 {
			if index := strings.IndexByte("*?[", unit.text[0]); index >= 0 {
				pattern.WriteRune([]rune{globAny, globOne, globSet}[index])
				glob = true
				continue
			}
		}
		pattern.WriteString(unit.text)
	}
	word.text = text.String()
	if glob {
		word.pattern = strings.ToLower(pattern.String())
	}
	return word
}

// renderUnits writes a word for the quote-insensitive checks: what was quoted
// in single quotes (without quote characters of its own), substitutions as
// $( ), the rest as it was.
func renderUnits(units []shellUnit) string {
	var out strings.Builder
	open := false
	for _, unit := range units {
		if unit.quoted != open && !unit.opaque || unit.opaque && open {
			out.WriteByte('\'')
			open = !open
		}
		switch {
		case unit.opaque:
			out.WriteString(unit.canon)
		case unit.quoted:
			out.WriteString(strings.ReplaceAll(unit.text, "'", ""))
		default:
			out.WriteString(unit.text)
		}
	}
	if open {
		out.WriteByte('\'')
	}
	return out.String()
}

func activeUnit(unit shellUnit, c string) bool {
	return !unit.quoted && !unit.opaque && unit.text == c
}

// expandBraces does bash's brace expansion on the units of a word: a{b,c}d
// and {x..y[..step]}. A word that would give more than budget words stands
// for any name, as a glob.
func expandBraces(units []shellUnit, budget *int) [][]shellUnit {
	for open := range units {
		if !activeUnit(units[open], "{") {
			continue
		}
		close := braceClose(units, open)
		if close < 0 {
			continue
		}
		amble := units[open+1 : close]
		var tack [][]shellUnit
		if hasComma(amble) {
			for _, part := range braceParts(amble) {
				tack = append(tack, expandBraces(part, budget)...)
			}
		} else if tack = braceSequence(amble, budget); tack == nil {
			if close+1 == len(units) {
				return [][]shellUnit{units}
			}
			tack = [][]shellUnit{units[open : close+1]}
		}
		post := expandBraces(units[close+1:], budget)
		*budget -= len(tack) * len(post)
		if *budget < 0 {
			return [][]shellUnit{append(append(append([]shellUnit{}, units[:open]...), shellUnit{text: "*"}), units[close+1:]...)}
		}
		out := [][]shellUnit{}
		for _, middle := range tack {
			for _, end := range post {
				word := append(append(append([]shellUnit{}, units[:open]...), middle...), end...)
				out = append(out, word)
			}
		}
		return out
	}
	return [][]shellUnit{units}
}

// braceClose finds the } that closes the { at open for an expansion: bash
// takes it only once a comma or .. has appeared at that level.
func braceClose(units []shellUnit, open int) int {
	level, separated := 0, false
	for i := open + 1; i < len(units); i++ {
		switch unit := units[i]; {
		case activeUnit(unit, "{"):
			level++
		case activeUnit(unit, "}"):
			if level == 0 && separated {
				return i
			}
			if level > 0 {
				level--
			}
		case activeUnit(unit, ","):
			separated = separated || level == 0
		case level == 0 && !unit.quoted && !unit.opaque:
			if at := strings.Index(unit.text, ".."); at >= 0 && (at+2 < len(unit.text) || i+1 < len(units) && !activeUnit(units[i+1], "}")) {
				separated = true
			}
		}
	}
	return -1
}

func hasComma(units []shellUnit) bool {
	for _, unit := range units {
		if activeUnit(unit, ",") {
			return true
		}
	}
	return false
}

func braceParts(units []shellUnit) [][]shellUnit {
	parts, level, start := [][]shellUnit{}, 0, 0
	for i, unit := range units {
		switch {
		case activeUnit(unit, "{"):
			level++
		case activeUnit(unit, "}") && level > 0:
			level--
		case activeUnit(unit, ",") && level == 0:
			parts = append(parts, units[start:i])
			start = i + 1
		}
	}
	return append(parts, units[start:])
}

func braceSequence(units []shellUnit, budget *int) [][]shellUnit {
	var text strings.Builder
	for _, unit := range units {
		if unit.quoted || unit.opaque {
			return nil
		}
		text.WriteString(unit.text)
	}
	parts := strings.Split(text.String(), "..")
	if len(parts) != 2 && len(parts) != 3 {
		return nil
	}
	step := 1
	if len(parts) == 3 {
		value, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil
		}
		step = max(value, -value, 1)
	}
	values := []string{}
	if first, err := strconv.Atoi(parts[0]); err == nil {
		last, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil
		}
		width := 0
		for _, part := range parts[:2] {
			if digits := strings.TrimLeft(part, "+-"); len(digits) > 1 && digits[0] == '0' {
				width = max(width, len(part))
			}
		}
		for value := first; (first <= last && value <= last || first > last && value >= last) && len(values) <= *budget; value += step * sign(last-first) {
			values = append(values, zeroPad(value, width))
		}
	} else if len(parts[0]) == 1 && len(parts[1]) == 1 && isLetter(parts[0][0]) && isLetter(parts[1][0]) {
		first, last := int(parts[0][0]), int(parts[1][0])
		for value := first; (first <= last && value <= last || first > last && value >= last) && len(values) <= *budget; value += step * sign(last-first) {
			values = append(values, string(rune(value)))
		}
	} else {
		return nil
	}
	out := [][]shellUnit{}
	for _, value := range values {
		out = append(out, []shellUnit{{text: value}})
	}
	return out
}

func sign(value int) int {
	if value < 0 {
		return -1
	}
	return 1
}

func zeroPad(value, width int) string {
	text := strconv.Itoa(max(value, -value))
	for len(text) < width-boolInt(value < 0) {
		text = "0" + text
	}
	if value < 0 {
		return "-" + text
	}
	return text
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// globMatch matches name against a pattern of shellArgument. A bracket
// expression stands for any one character. Everything but a star takes one
// character, so on a mismatch only the last star needs to take one more, and a
// run of stars no longer multiplies the tries: trying every way to share the
// name among the stars took some 20 seconds for 31 stars before
// "noctis.exe".
func globMatch(pattern, name string) bool {
	p, n := []rune(pattern), []rune(name)
	i, j := 0, 0
	star, from := -1, 0
	for j < len(n) {
		if i < len(p) {
			switch p[i] {
			case globAny:
				star, from = i, j
				i++
				continue
			case globOne:
				i, j = i+1, j+1
				continue
			case globSet:
				if end := bracketEnd(p, i); end >= 0 {
					i, j = end+1, j+1
					continue
				}
				if n[j] == '[' {
					i, j = i+1, j+1
					continue
				}
			default:
				if p[i] == n[j] {
					i, j = i+1, j+1
					continue
				}
			}
		}
		if star < 0 {
			return false
		}
		from++
		i, j = star+1, from
	}
	for i < len(p) && p[i] == globAny {
		i++
	}
	return i == len(p)
}

func bracketEnd(p []rune, open int) int {
	i := open + 1
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		i++
	}
	if i < len(p) && p[i] == ']' {
		i++
	}
	for ; i < len(p); i++ {
		if (p[i] == '[' || p[i] == globSet) && i+1 < len(p) && strings.ContainsRune(":.=", p[i+1]) {
			for k := i + 2; k+1 < len(p); k++ {
				if p[k] == p[i+1] && p[k+1] == ']' {
					i = k + 1
					break
				}
			}
			continue
		}
		if p[i] == ']' {
			return i
		}
	}
	return -1
}

// namesNoctis tells whether word runs the noctis binary: by name or path, or
// as a glob that can name it.
func namesNoctis(word shellArgument) bool {
	if plain, path := wordPrograms(word.text); plain == pluginName || path == pluginName {
		return true
	}
	if word.pattern == "" {
		return false
	}
	name := word.pattern[strings.LastIndexAny(word.pattern, `/\`)+1:]
	return globMatch(name, pluginName) || globMatch(name, pluginName+".exe")
}

// argumentWords gives what noctis reads of each word: its plain text, or the
// glob, marked with a leading NUL, that the shell would expand.
func argumentWords(words []shellArgument) []string {
	out := []string{}
	for _, word := range words {
		if word.pattern != "" {
			out = append(out, "\x00"+word.pattern)
		} else {
			out = append(out, plainWord(word.text))
		}
	}
	return out
}

// startsWithWords tells whether words can start with want; a glob may stand
// for one or more of them, as it expands to every name it matches.
func startsWithWords(words, want []string) bool {
	if len(want) == 0 {
		return true
	}
	if len(words) == 0 {
		return false
	}
	pattern, glob := strings.CutPrefix(words[0], "\x00")
	if !glob {
		return words[0] == want[0] && startsWithWords(words[1:], want[1:])
	}
	for taken := 1; taken <= len(want) && globMatch(pattern, want[taken-1]); taken++ {
		if startsWithWords(words[1:], want[taken:]) {
			return true
		}
	}
	return false
}
