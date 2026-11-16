package ownership

import (
	"fmt"
	"strings"
	"unicode"
)

type sqlToken struct {
	text     string
	literal  bool
	argument int
	quoted   bool
}

var approvedSQLFunctions = map[string]bool{
	"coalesce": true, "count": true, "instr": true, "json_array_length": true, "json_extract": true, "json_type": true, "json_valid": true, "length": true, "max": true, "min": true, "printf": true, "raise": true, "replace": true, "rtrim": true, "substr": true, "sum": true, "trim": true, "typeof": true, "upper": true,
}
var sqlParentheses = map[string]bool{"and": true, "or": true, "not": true, "in": true, "exists": true, "cast": true, "select": true, "from": true, "join": true, "where": true, "when": true, "then": true, "else": true, "on": true, "using": true, "values": true, "filter": true}

func isCatalog(name string) bool { return IsCatalog(name) || name == "pragma_table_list" }

func sqlTokens(source string) ([]sqlToken, error) {
	var tokens []sqlToken
	arguments := 0
	for i := 0; i < len(source); {
		c := source[i]
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if c == '-' && i+1 < len(source) && source[i+1] == '-' {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(source) && source[i+1] == '*' {
			end := strings.Index(source[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("unterminated SQL comment")
			}
			i += end + 4
			continue
		}
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			endChar := c
			if c == '[' {
				endChar = ']'
			}
			i++
			var value strings.Builder
			closed := false
			for i < len(source) {
				if source[i] == endChar {
					i++
					if i < len(source) && source[i] == endChar {
						value.WriteByte(endChar)
						i++
						continue
					}
					closed = true
					break
				}
				value.WriteByte(source[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated SQL quote")
			}
			tokens = append(tokens, sqlToken{text: strings.ToLower(value.String()), literal: c == '\'', quoted: c != '\''})
			continue
		}
		if unicode.IsLetter(rune(c)) || c == '_' {
			start := i
			i++
			for i < len(source) && (unicode.IsLetter(rune(source[i])) || unicode.IsDigit(rune(source[i])) || source[i] == '_') {
				i++
			}
			tokens = append(tokens, sqlToken{text: strings.ToLower(source[start:i])})
			continue
		}
		if c == ':' || c == '@' || c == '$' {
			return nil, fmt.Errorf("named SQL parameters are unsupported")
		}
		if c == '?' {
			if i+1 < len(source) && unicode.IsDigit(rune(source[i+1])) {
				return nil, fmt.Errorf("numbered SQL parameters are unsupported")
			}
			tokens = append(tokens, sqlToken{text: "?", argument: arguments})
			arguments++
			i++
			continue
		}
		tokens = append(tokens, sqlToken{text: string(c)})
		i++
	}
	return tokens, nil
}

func tableAt(tokens []sqlToken, index int) (string, int, error) {
	if index >= len(tokens) {
		return "", index, fmt.Errorf("missing SQL table")
	}
	name := tokens[index]
	if name.literal || name.text == "" || !(unicode.IsLetter(rune(name.text[0])) || name.text[0] == '_') {
		return "", index, fmt.Errorf("unsupported or dynamic SQL table %q", name.text)
	}
	index++
	if index < len(tokens) && tokens[index].text == "." {
		if name.text != "main" {
			return "", index, fmt.Errorf("unsupported SQL schema %s", name.text)
		}
		return tableAt(tokens, index+1)
	}

	return name.text, index, nil
}

type sqlReference struct {
	name   string
	object bool
}

func sqlTableReferences(tokens []sqlToken) ([]sqlReference, bool, error) {
	return sqlTableReferencesMode(tokens, false)
}

func sqlTableReferencesMode(tokens []sqlToken, maintenanceMetadata bool) ([]sqlReference, bool, error) {
	if len(tokens) == 0 || tokens[0].quoted {
		return nil, false, fmt.Errorf("empty SQL")
	}
	var tables []sqlReference
	write := false
	switch tokens[0].text {
	case "select":
	case "insert", "replace", "update", "delete":
		write = true
	case "pragma":
		if len(tokens) != 5 || tokens[2].text != "(" || tokens[4].text != ")" {
			return nil, false, fmt.Errorf("unsupported PRAGMA")
		}
		switch tokens[1].text {
		case "table_info", "table_xinfo", "foreign_key_list", "index_list", "index_info", "index_xinfo":
		default:
			return nil, false, fmt.Errorf("unsupported PRAGMA")
		}
		name, _, err := tableAt(tokens[3:4], 0)
		return []sqlReference{{name: name, object: tokens[1].text == "index_info" || tokens[1].text == "index_xinfo"}}, false, err
	default:
		return nil, false, fmt.Errorf("unsupported SQL form %s", tokens[0].text)
	}
	depth := 0
	from := map[int]bool{}
	catalogAtDepth := map[int]bool{}
	tablesAtDepth := map[int]int{}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !token.literal && i+1 < len(tokens) && tokens[i+1].text == "(" && (token.quoted || len(token.text) > 0 && (unicode.IsLetter(rune(token.text[0])) || token.text[0] == '_')) {
			if !approvedSQLFunctions[token.text] && (token.quoted || !sqlParentheses[token.text]) {
				return nil, false, fmt.Errorf("unapproved SQL function %s", token.text)
			}
		}
		if token.literal || token.quoted {
			continue
		}
		switch token.text {
		case ";":
			if i != len(tokens)-1 {
				return nil, false, fmt.Errorf("multiple SQL statements")
			}
		case "in":
			if i+1 >= len(tokens) || tokens[i+1].text != "(" {
				return nil, false, fmt.Errorf("unsupported IN table reference")
			}
		case "with", "attach", "detach", "returning", ":", "@", "$":
			return nil, false, fmt.Errorf("unsupported SQL form %s", token.text)
		case "(":
			depth++
		case ")":
			delete(from, depth)
			delete(catalogAtDepth, depth)
			delete(tablesAtDepth, depth)
			depth--
			if depth < 0 {
				return nil, false, fmt.Errorf("unbalanced SQL")
			}
		case "union", "except", "intersect":
			from[depth] = false
			delete(catalogAtDepth, depth)
			delete(tablesAtDepth, depth)
		case "where", "group", "order", "having", "limit", "values", "set":
			from[depth] = false
		case "from", "join", "into", "update", ",":
			table := token.text == "from" || token.text == "join" || token.text == "into" || token.text == "update" || token.text == "," && from[depth]
			if !table {
				continue
			}
			if i+1 < len(tokens) && tokens[i+1].text == "(" {
				if (token.text != "from" && token.text != "join") || i+2 >= len(tokens) || tokens[i+2].text != "select" {
					return nil, false, fmt.Errorf("unsupported SQL table expression")
				}
				from[depth] = true
				tablesAtDepth[depth]++
				continue
			}
			index := i + 1
			if token.text == "update" && index+1 < len(tokens) && tokens[index].text == "or" {
				index += 2
			}
			name, next, err := tableAt(tokens, index)
			if err != nil {
				return nil, false, err
			}
			object := false
			if next < len(tokens) && tokens[next].text == "(" && (token.text == "from" || token.text == "join" || token.text == ",") {
				switch name {
				case "json_each":
					if next+4 >= len(tokens) || (tokens[next+1].text != "new" && tokens[next+1].text != "old") || tokens[next+2].text != "." || tokens[next+4].text != ")" {
						return nil, false, fmt.Errorf("unsupported JSON table function source")
					}
					name = ""
					next += 5
				case "pragma_table_info", "pragma_table_xinfo", "pragma_foreign_key_list", "pragma_index_list", "pragma_index_info", "pragma_index_xinfo":
					if maintenanceMetadata {
						if next+2 >= len(tokens) || (!tokens[next+1].literal && tokens[next+1].text != "?") || tokens[next+2].text != ")" {
							return nil, false, fmt.Errorf("metadata function requires an object literal or bound name")
						}
						if tokens[next+1].literal {
							if _, known := ObjectOwner(tokens[next+1].text); !known {
								return nil, false, fmt.Errorf("unregistered metadata object %s", tokens[next+1].text)
							}
						}
						name = "sqlite_master"
						next += 3
						break
					}
					if next+2 >= len(tokens) || !tokens[next+1].literal || tokens[next+2].text != ")" {
						return nil, false, fmt.Errorf("schema table function requires fixed owned object literal")
					}
					object = name == "pragma_index_info" || name == "pragma_index_xinfo"
					name = tokens[next+1].text
					next += 3
				default:
					return nil, false, fmt.Errorf("unsupported table-valued function %s", name)
				}
			}
			if maintenanceMetadata && name == "pragma_foreign_key_check" {
				name = "sqlite_master"
			}
			if isCatalog(name) {
				if token.text != "from" || tablesAtDepth[depth] > 0 {
					return nil, false, fmt.Errorf("catalog joins are unsupported")
				}
				catalogAtDepth[depth] = true
			} else if catalogAtDepth[depth] {
				return nil, false, fmt.Errorf("catalog joins are unsupported")
			}
			tablesAtDepth[depth]++
			if name != "" {
				tables = append(tables, sqlReference{name: name, object: object})
			}
			i = next - 1
			from[depth] = true
		}
	}
	if depth != 0 {
		return nil, false, fmt.Errorf("unbalanced SQL")
	}

	return tables, write, nil
}

func ValidateSQL(owner, source string, catalogObjects []string) error {
	tokens, err := sqlTokens(source)
	if err != nil {
		return err
	}
	return validateSQLTokens(owner, tokens, catalogObjects, false)
}

func validateSQLTokens(owner string, tokens []sqlToken, catalogObjects []string, allowScalar bool) error {
	tables, write, err := sqlTableReferences(tokens)
	if err != nil {
		return err
	}
	if len(tables) == 0 && (!allowScalar || len(tokens) == 0 || tokens[0].text != "select") {
		return fmt.Errorf("SQL has no attributable table")
	}
	for _, reference := range tables {
		table := reference.name
		if isCatalog(table) {
			if write {
				return fmt.Errorf("catalog is read only")
			}
			if owner != "maintenance" {
				if err := checkCatalogPredicate(owner, tokens, catalogObjects); err != nil {
					return err
				}
			}
			continue
		}
		expected, ok := Owner(table)
		if !ok && reference.object {
			expected, ok = ObjectOwner(table)
		}
		if !ok {
			return fmt.Errorf("unregistered SQL object %s", table)
		}
		if table == ControlTable && !write {
			continue
		}
		if expected != owner {
			return fmt.Errorf("SQL object %s owned by %s, accessed by %s", table, expected, owner)
		}
		if owner == "maintenance" && table != ControlTable && write {
			return fmt.Errorf("maintenance cannot write business table %s", table)
		}
	}
	return nil
}

func checkCatalogPredicate(owner string, tokens []sqlToken, objects []string) error {
	_, err := catalogPredicates(owner, tokens, objects, false)
	return err
}

func CatalogArguments(source string) ([]int, error) {
	tokens, err := sqlTokens(source)
	if err != nil {
		return nil, err
	}
	return catalogPredicates("", tokens, nil, true)
}

func catalogPredicates(owner string, tokens []sqlToken, objects []string, collect bool) ([]int, error) {
	var arguments []int
	depth := 0
	for i, token := range tokens {
		if token.literal || token.quoted {
			continue
		}
		if token.text == "(" {
			depth++
			continue
		}
		if token.text == ")" {
			depth--
			continue
		}
		if token.text != "from" {
			continue
		}
		name, next, err := tableAt(tokens, i+1)
		if err != nil || !isCatalog(name) {
			continue
		}
		qualifier := name
		if next < len(tokens) && tokens[next].text == "as" {
			next++
			if next < len(tokens) {
				qualifier = tokens[next].text
				next++
			}
		} else if next < len(tokens) && tokens[next].text != "where" && tokens[next].text != ";" {
			qualifier = tokens[next].text
			next++
		}
		branchDepth := depth
		where := -1
		end := len(tokens)
		scanDepth := depth
		for at := next; at < len(tokens); at++ {
			text := tokens[at].text
			if tokens[at].literal {
				continue
			}
			if text == "(" {
				scanDepth++
			}
			if text == ")" {
				scanDepth--
				if scanDepth < branchDepth {
					end = at
					break
				}
			}
			if scanDepth != branchDepth {
				continue
			}
			if where < 0 && (text == "join" || text == ",") {
				return nil, fmt.Errorf("catalog joins are unsupported")
			}
			if text == "where" {
				where = at + 1
				continue
			}
			if text == "union" || text == "except" || text == "intersect" || text == "group" || text == "order" || text == "limit" || text == ";" {
				end = at
				break
			}
		}
		if where < 0 || where >= end {
			return nil, fmt.Errorf("unbounded catalog read")
		}
		parser := catalogPredicate{owner: owner, tokens: tokens[where:end], objects: objects, collect: collect, qualifier: qualifier, table: name}
		bounded, err := parser.or()
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, parser.arguments...)
		required := uint8(1)
		if name == "pragma_table_list" {
			required = 7
		}
		if parser.at != len(parser.tokens) || bounded&required != required {
			return nil, fmt.Errorf("catalog read lacks owned object restriction")
		}
	}
	return arguments, nil
}

type catalogPredicate struct {
	owner     string
	tokens    []sqlToken
	objects   []string
	at        int
	collect   bool
	qualifier string
	arguments []int
	table     string
}

func (p *catalogPredicate) take(text string) bool {
	if p.at < len(p.tokens) && !p.tokens[p.at].literal && p.tokens[p.at].text == text {
		p.at++
		return true
	}
	return false
}
func (p *catalogPredicate) or() (uint8, error) {
	bounded, err := p.and()
	if err != nil {
		return 0, err
	}
	for p.take("or") {
		right, err := p.and()
		if err != nil {
			return 0, err
		}
		bounded = bounded & right
	}
	return bounded, nil
}
func (p *catalogPredicate) and() (uint8, error) {
	bounded, err := p.atom()
	if err != nil {
		return 0, err
	}
	for p.take("and") {
		right, err := p.atom()
		if err != nil {
			return 0, err
		}
		bounded = bounded | right
	}
	return bounded, nil
}
func (p *catalogPredicate) atom() (uint8, error) {
	if p.take("(") {
		bounded, err := p.or()
		if err != nil {
			return 0, err
		}
		if !p.take(")") {
			return 0, fmt.Errorf("unterminated catalog predicate")
		}
		return bounded, nil
	}
	if p.at >= len(p.tokens) {
		return 0, fmt.Errorf("missing catalog predicate")
	}
	key := p.tokens[p.at].text
	p.at++
	if p.take(".") {
		if key != p.qualifier {
			return 0, fmt.Errorf("catalog predicate uses another table qualifier %s", key)
		}
		if p.at >= len(p.tokens) {
			return 0, fmt.Errorf("missing catalog field")
		}
		key = p.tokens[p.at].text
		p.at++
	}
	if key != "name" && key != "tbl_name" && key != "type" && !(p.table == "pragma_table_list" && key == "schema") {
		return 0, fmt.Errorf("unsupported catalog predicate %s", key)
	}
	var values []sqlToken
	if p.take("=") {
		if p.at >= len(p.tokens) {
			return 0, fmt.Errorf("missing catalog value")
		}
		values = append(values, p.tokens[p.at])
		p.at++
	} else if p.take("in") && p.take("(") {
		for {
			if p.at >= len(p.tokens) {
				return 0, fmt.Errorf("missing catalog IN value")
			}
			values = append(values, p.tokens[p.at])
			p.at++
			if p.take(",") {
				continue
			}
			if !p.take(")") {
				return 0, fmt.Errorf("unsupported catalog IN")
			}
			break
		}
	} else {
		return 0, fmt.Errorf("unsupported catalog comparison")
	}
	for _, value := range values {
		if p.table == "pragma_table_list" && (key == "schema" || key == "type") {
			expected := "main"
			if key == "type" {
				expected = "table"
			}
			if !value.literal || value.text != expected {
				return 0, fmt.Errorf("pragma_table_list requires main table predicate")
			}
			continue
		}
		if key == "type" {
			if !value.literal && value.text != "?" {
				return 0, fmt.Errorf("dynamic catalog type")
			}
			continue
		}
		names := []string{value.text}
		if value.text == "?" && !value.literal {
			if p.table == "pragma_table_list" {
				return 0, fmt.Errorf("pragma_table_list requires fixed object literal")
			}
			p.arguments = append(p.arguments, value.argument)
			if !p.collect && len(p.objects) == 0 {
				return 0, fmt.Errorf("unbounded catalog object argument")
			}
			names = p.objects
		} else if !value.literal {
			return 0, fmt.Errorf("dynamic catalog object")
		}
		for _, name := range names {
			if p.collect {
				continue
			}
			expected, ok := ObjectOwner(name)
			if !ok || expected != p.owner {
				return 0, fmt.Errorf("catalog object %s is not owned by %s", name, p.owner)
			}
		}
	}
	if key == "schema" {
		return 2, nil
	}
	if key == "type" {
		if p.table == "pragma_table_list" {
			return 4, nil
		}
		return 0, nil
	}
	return 1, nil
}

func ValidateSchema(source string) ([]string, error) {
	var tables []string
	tokens, err := sqlTokens(source)
	if err != nil {
		return nil, err
	}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].literal || tokens[i].quoted || tokens[i].text != "create" {
			continue
		}
		index := i + 1
		for index < len(tokens) && (tokens[index].text == "unique" || tokens[index].text == "temp" || tokens[index].text == "temporary") {
			index++
		}
		if index >= len(tokens) {
			return nil, fmt.Errorf("incomplete schema declaration")
		}
		kind := tokens[index].text
		index++
		if index+2 < len(tokens) && tokens[index].text == "if" && tokens[index+1].text == "not" && tokens[index+2].text == "exists" {
			index += 3
		}
		if index >= len(tokens) {
			return nil, fmt.Errorf("missing schema object")
		}
		name := tokens[index].text
		expected, ok := Owner(name)
		if kind == "trigger" || kind == "index" {
			expected, ok = ObjectOwner(name)
		}
		if !ok {
			return nil, fmt.Errorf("unregistered schema %s %s", kind, name)
		}
		if kind == "view" {
			return nil, fmt.Errorf("view requires explicit registry owner %s", name)
		}
		if kind == "table" {
			tables = append(tables, name)
			for at := index + 1; at < len(tokens) && tokens[at].text != ";"; at++ {
				if tokens[at].literal {
					continue
				}
				if tokens[at].text == "references" {
					table, _, err := tableAt(tokens, at+1)
					if err != nil {
						return nil, err
					}
					actual, known := Owner(table)
					if !known || actual != expected {
						return nil, fmt.Errorf("table %s references other owner object %s", name, table)
					}
				}
				if tokens[at].text == "select" {
					end := at
					for end < len(tokens) && tokens[end].text != ";" {
						end++
					}
					if err := validateSQLTokens(expected, tokens[at:end], nil, true); err != nil {
						return nil, err
					}
					break
				}
			}
			continue
		}
		if kind != "index" && kind != "trigger" {
			return nil, fmt.Errorf("unsupported schema object %s", kind)
		}
		on := index + 1
		for on < len(tokens) && tokens[on].text != "on" {
			on++
		}
		target, _, err := tableAt(tokens, on+1)
		if err != nil {
			return nil, err
		}
		targetOwner, known := Owner(target)
		if !known || targetOwner != expected {
			return nil, fmt.Errorf("schema object %s targets other owner %s", name, target)
		}
		if kind == "trigger" {
			body := on + 1
			for body < len(tokens) && tokens[body].text != "begin" {
				body++
			}
			if body == len(tokens) {
				return nil, fmt.Errorf("trigger %s lacks body", name)
			}
			header := append([]sqlToken{{text: "select"}}, tokens[on+2:body]...)
			if err := validateSQLTokens(expected, header, nil, true); err != nil {
				return nil, err
			}
			end := body + 1
			cases := 0
			for end < len(tokens) {
				if !tokens[end].literal {
					if tokens[end].text == "case" {
						cases++
					}
					if tokens[end].text == "end" {
						if cases == 0 {
							break
						}
						cases--
					}
				}
				end++
			}
			if end == len(tokens) {
				return nil, fmt.Errorf("unterminated trigger %s", name)
			}
			for start := body + 1; start < end; {
				stop := start
				for stop < end && (tokens[stop].text != ";" || tokens[stop].literal) {
					stop++
				}
				if stop > start {
					if err := validateSQLTokens(expected, tokens[start:stop], nil, true); err != nil {
						return nil, fmt.Errorf("trigger %s: %w", name, err)
					}
				}
				start = stop + 1
			}
		}
	}
	return tables, nil
}

func ValidateOwnerPriceWrite(source string) error {
	tokens, err := sqlTokens(source)
	if err != nil {
		return err
	}
	if len(tokens) < 3 || tokens[0].quoted || tokens[0].text != "insert" || tokens[1].quoted || tokens[1].text != "into" {
		return fmt.Errorf("owner-price write requires fixed INSERT INTO")
	}
	target, index, err := tableAt(tokens, 2)
	if err != nil {
		return err
	}
	if target != "owner_price_series_versions" {
		return fmt.Errorf("owner-price write targets unapproved table %s", target)
	}
	if index >= len(tokens) || tokens[index].text != "(" {
		return fmt.Errorf("owner-price write requires fixed columns")
	}
	index++
	columns := 0
	for {
		if index >= len(tokens) || tokens[index].literal || tokens[index].text == "" || !(unicode.IsLetter(rune(tokens[index].text[0])) || tokens[index].text[0] == '_') {
			return fmt.Errorf("invalid owner-price column")
		}
		columns++
		index++
		if index < len(tokens) && tokens[index].text == "," {
			index++
			continue
		}
		if index >= len(tokens) || tokens[index].text != ")" {
			return fmt.Errorf("invalid owner-price columns")
		}
		index++
		break
	}
	if index+1 >= len(tokens) || tokens[index].quoted || tokens[index].text != "values" || tokens[index+1].text != "(" {
		return fmt.Errorf("owner-price write requires bound VALUES")
	}
	index += 2
	arguments := 0
	for {
		if index >= len(tokens) || tokens[index].text != "?" || tokens[index].literal {
			return fmt.Errorf("owner-price VALUES must contain only bound arguments")
		}
		arguments++
		index++
		if index < len(tokens) && tokens[index].text == "," {
			index++
			continue
		}
		if index >= len(tokens) || tokens[index].text != ")" {
			return fmt.Errorf("invalid owner-price VALUES")
		}
		index++
		break
	}
	if columns != arguments || index != len(tokens) {
		return fmt.Errorf("owner-price write has unapproved shape")
	}
	return validateSQLTokens("marketdata", tokens, nil, false)
}

// ValidateMaintenanceCatalogRead checks physical metadata only; callers must also bind it to the reviewed maintenance executor and object provenance.
func ValidateMaintenanceCatalogRead(source string) error {
	tokens, err := sqlTokens(source)
	if err != nil {
		return err
	}
	if len(tokens) == 0 || tokens[0].text != "select" || tokens[0].quoted {
		return fmt.Errorf("maintenance metadata requires SELECT")
	}
	references, write, err := sqlTableReferencesMode(tokens, true)
	if err != nil {
		return err
	}
	if write || len(references) == 0 {
		return fmt.Errorf("maintenance metadata has no catalog")
	}
	for _, reference := range references {
		if !isCatalog(reference.name) {
			return fmt.Errorf("maintenance metadata cannot read business table %s", reference.name)
		}
	}
	return nil
}
