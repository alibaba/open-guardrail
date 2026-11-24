package ctlcmd

import "strings"

// isStrictReadOnlySQL recognizes a conservative lexical subset, not a SQL
// dialect. Unknown syntax and function calls require a guardrail assessment.
func isStrictReadOnlySQL(sql string) bool {
	tokens, ok := mcpSQLTokens(sql)
	if !ok || len(tokens) == 0 {
		return false
	}
	if tokens[len(tokens)-1] == ";" {
		tokens = tokens[:len(tokens)-1]
	}
	if len(tokens) == 0 {
		return false
	}
	switch tokens[0] {
	case "SELECT", "EXPLAIN", "SHOW", "DESCRIBE", "DESC", "VALUES", "WITH":
	default:
		return false
	}
	depth := 0
	for i, token := range tokens {
		switch token {
		case ";", "INSERT", "UPDATE", "DELETE", "MERGE", "REPLACE", "UPSERT", "DROP", "ALTER", "TRUNCATE", "GRANT", "REVOKE", "COPY", "CALL", "EXEC", "EXECUTE", "DO", "SET", "LOCK", "UNLOCK", "VACUUM", "ATTACH", "DETACH", "PRAGMA", "LOAD", "IMPORT", "INTO", "FOR", "ANALYZE", "ANALYSE", ":=":
			return false
		case "CREATE":
			if i != 1 || tokens[0] != "SHOW" || len(tokens) < 4 || tokens[2] != "TABLE" {
				return false
			}
		case "(":
			depth++
		case ")":
			depth--
			if depth < 0 {
				return false
			}
		}
		// SELECT can invoke mutating UDFs, sequence operations or advisory locks.
		// Allow only SQL grammar words and standard pure aggregate/null helpers
		// before '(' in this minimal recognizer.
		if i+1 < len(tokens) && tokens[i+1] == "(" && mcpSQLWord(token) {
			if i > 0 && tokens[i-1] == "." {
				return false // Qualified names are function calls, not grammar.
			}
			switch token {
			case "AS", "IN", "EXISTS", "NOT", "AND", "OR", "WHERE", "ON", "SELECT", "VALUES", "BY", "HAVING", "WHEN", "THEN", "ELSE", "FROM", "JOIN", "USING", "OVER", "EXPLAIN",
				"COUNT", "SUM", "AVG", "MIN", "MAX", "COALESCE", "NULLIF":
			default:
				return false
			}
		}
	}
	return depth == 0
}

func mcpSQLWord(token string) bool {
	return len(token) > 0 && (mcpSQLIdentifier(token[0]) || token == "<identifier>")
}

func mcpSQLIdentifier(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_'
}

func mcpSQLTokens(sql string) ([]string, bool) {
	var tokens []string
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f':
			i++
		case c == '#':
			return nil, false // A comment in MySQL, an operator in other dialects.
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			if i+2 < len(sql) && sql[i+2] != ' ' && sql[i+2] != '\t' && sql[i+2] != '\r' && sql[i+2] != '\n' {
				return nil, false // MySQL requires whitespace after '--'.
			}
			for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			if i+2 < len(sql) && sql[i+2] == '!' || i+3 < len(sql) && (sql[i+2] == 'M' || sql[i+2] == 'm') && sql[i+3] == '!' {
				return nil, false // MySQL executable comments are SQL, not comments.
			}
			i += 2
			depth := 1
			for i < len(sql) && depth > 0 {
				if i+1 < len(sql) && sql[i:i+2] == "/*" {
					// Nested comments have incompatible meanings across dialects.
					return nil, false
				} else if i+1 < len(sql) && sql[i:i+2] == "*/" {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return nil, false
			}
		case c == '\'' || c == '"' || c == '`':
			quote := c
			i++
			closed := false
			for i < len(sql) {
				if sql[i] == '\\' {
					// Backslash quoting depends on server SQL mode. Route ambiguous
					// strings through the gate rather than guessing that mode.
					return nil, false
				}
				if sql[i] == quote {
					i++
					if i < len(sql) && sql[i] == quote {
						i++
						continue
					}
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, false
			}
			if quote == '\'' {
				tokens = append(tokens, "<literal>")
			} else {
				tokens = append(tokens, "<identifier>")
			}
		case c == '$':
			end := i + 1
			for end < len(sql) && (mcpSQLIdentifier(sql[end]) || end > i+1 && sql[end] >= '0' && sql[end] <= '9') {
				end++
			}
			if end >= len(sql) || sql[end] != '$' {
				return nil, false
			}
			delimiter := sql[i : end+1]
			close := strings.Index(sql[end+1:], delimiter)
			if close < 0 {
				return nil, false
			}
			i = end + 1 + close + len(delimiter)
			tokens = append(tokens, "<literal>")
		case mcpSQLIdentifier(c):
			start := i
			for i < len(sql) && (mcpSQLIdentifier(sql[i]) || sql[i] >= '0' && sql[i] <= '9' || sql[i] == '$') {
				i++
			}
			tokens = append(tokens, strings.ToUpper(sql[start:i]))
		case c >= '0' && c <= '9':
			start := i
			for i < len(sql) && (sql[i] >= '0' && sql[i] <= '9' || sql[i] == '.') {
				i++
			}
			tokens = append(tokens, sql[start:i])
		case c == ':' && i+1 < len(sql) && sql[i+1] == '=':
			tokens = append(tokens, ":=")
			i += 2
		case strings.ContainsRune("(),;.*+-/%=<>!|&:^?", rune(c)):
			tokens = append(tokens, string(c))
			i++
		default:
			return nil, false
		}
	}
	return tokens, true
}
