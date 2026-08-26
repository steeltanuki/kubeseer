// Copyright 2026 Alessandro Rontani
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package extraction

import (
	"strconv"
	"strings"
)

type operationKind uint8

const (
	operationProperty operationKind = iota
	operationIndex
	operationWildcard
)

type operation struct {
	kind  operationKind
	key   string
	index int
}

func parsePath(sourceID, fieldName, path string) ([]operation, *ExtractionError) {
	if path == "" {
		return nil, invalidExpression(sourceID, fieldName, "path is empty", 0)
	}
	if hasTextOutsideSingleExpression(path) {
		return nil, unsupportedExpression(sourceID, fieldName, "text outside the single delimited path", 0)
	}
	if len(path) < 2 || path[0] != '{' || path[len(path)-1] != '}' {
		return nil, invalidExpression(sourceID, fieldName, "path must have one opening and closing delimiter", 0)
	}
	body := path[1 : len(path)-1]
	if strings.ContainsAny(body, "{}") {
		return nil, unsupportedExpression(sourceID, fieldName, "multiple or nested path delimiters are unsupported", 1)
	}
	if strings.HasPrefix(body, "..") {
		return nil, unsupportedExpression(sourceID, fieldName, "recursive descent is unsupported", 1)
	}
	if body == "" || body[0] != '.' {
		return nil, invalidExpression(sourceID, fieldName, "path must address the resource root with a leading dot", 1)
	}

	parser := pathParser{sourceID: sourceID, fieldName: fieldName, input: body}
	parser.position = 1
	var operations []operation
	for parser.position < len(body) {
		var op operation
		var err *ExtractionError
		switch body[parser.position] {
		case '.':
			if parser.position+1 < len(body) && body[parser.position+1] == '.' {
				return nil, unsupportedExpression(sourceID, fieldName, "recursive descent is unsupported", parser.position+1)
			}
			parser.position++
			op, err = parser.parseIdentifier()
		case '[':
			op, err = parser.parseBracket()
		default:
			if isIdentifierStart(body[parser.position]) {
				op, err = parser.parseIdentifier()
				break
			}
			if unsupportedTokenAt(body, parser.position) {
				return nil, unsupportedExpression(sourceID, fieldName, "JSONPath construct is outside the supported subset", parser.position+1)
			}
			return nil, invalidExpression(sourceID, fieldName, "path contains an invalid token", parser.position+1)
		}
		if err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	return operations, nil
}

type pathParser struct {
	sourceID  string
	fieldName string
	input     string
	position  int
}

func (p *pathParser) parseIdentifier() (operation, *ExtractionError) {
	start := p.position
	if start >= len(p.input) || !isIdentifierStart(p.input[start]) {
		if start < len(p.input) && (p.input[start] == '*' || p.input[start] == '?' || p.input[start] == '|') {
			return operation{}, unsupportedExpression(p.sourceID, p.fieldName, "JSONPath construct is outside the supported subset", start+1)
		}
		return operation{}, invalidExpression(p.sourceID, p.fieldName, "property name is invalid", start+1)
	}
	p.position++
	for p.position < len(p.input) && isIdentifierPart(p.input[p.position]) {
		p.position++
	}
	return operation{kind: operationProperty, key: p.input[start:p.position]}, nil
}

func (p *pathParser) parseBracket() (operation, *ExtractionError) {
	start := p.position
	p.position++
	if p.position >= len(p.input) {
		return operation{}, invalidExpression(p.sourceID, p.fieldName, "bracket token is not closed", start+1)
	}

	switch p.input[p.position] {
	case '\'':
		return p.parseQuotedKey(start)
	case '"':
		return operation{}, unsupportedExpression(p.sourceID, p.fieldName, "double-quoted bracket keys are unsupported", p.position+1)
	case '*':
		p.position++
		if p.position >= len(p.input) || p.input[p.position] != ']' {
			return operation{}, invalidExpression(p.sourceID, p.fieldName, "wildcard bracket token is malformed", start+1)
		}
		p.position++
		return operation{kind: operationWildcard}, nil
	case '-':
		return operation{}, unsupportedExpression(p.sourceID, p.fieldName, "negative indexes are unsupported", p.position+1)
	case '?', ':', ',':
		return operation{}, unsupportedExpression(p.sourceID, p.fieldName, "filters, slices, and unions are unsupported", p.position+1)
	default:
		if !isASCIIDigit(p.input[p.position]) {
			if p.input[p.position] == ']' {
				return operation{}, invalidExpression(p.sourceID, p.fieldName, "bracket token is empty", p.position+1)
			}
			return operation{}, unsupportedExpression(p.sourceID, p.fieldName, "bracket construct is outside the supported subset", p.position+1)
		}
		return p.parseIndex(start)
	}
}

func (p *pathParser) parseQuotedKey(start int) (operation, *ExtractionError) {
	p.position++
	var key strings.Builder
	for p.position < len(p.input) {
		character := p.input[p.position]
		switch character {
		case '\'':
			p.position++
			if p.position >= len(p.input) || p.input[p.position] != ']' {
				return operation{}, invalidExpression(p.sourceID, p.fieldName, "quoted bracket key is not closed", start+1)
			}
			p.position++
			return operation{kind: operationProperty, key: key.String()}, nil
		case '\\':
			p.position++
			if p.position >= len(p.input) || (p.input[p.position] != '\\' && p.input[p.position] != '\'') {
				return operation{}, invalidExpression(p.sourceID, p.fieldName, "quoted bracket key contains an invalid escape", p.position+1)
			}
			key.WriteByte(p.input[p.position])
			p.position++
		default:
			key.WriteByte(character)
			p.position++
		}
	}
	return operation{}, invalidExpression(p.sourceID, p.fieldName, "quoted bracket key is not closed", start+1)
}

func (p *pathParser) parseIndex(start int) (operation, *ExtractionError) {
	indexStart := p.position
	for p.position < len(p.input) && isASCIIDigit(p.input[p.position]) {
		p.position++
	}
	if p.position >= len(p.input) || p.input[p.position] != ']' {
		if p.position < len(p.input) && (p.input[p.position] == ':' || p.input[p.position] == ',') {
			return operation{}, unsupportedExpression(p.sourceID, p.fieldName, "slices and unions are unsupported", p.position+1)
		}
		return operation{}, invalidExpression(p.sourceID, p.fieldName, "array index token is not closed", start+1)
	}
	text := p.input[indexStart:p.position]
	p.position++
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil || value > uint64(^uint(0)>>1) {
		return operation{}, invalidExpression(p.sourceID, p.fieldName, "array index is outside the supported range", indexStart+1)
	}
	return operation{kind: operationIndex, index: int(value)}, nil
}

func hasTextOutsideSingleExpression(path string) bool {
	if !strings.HasPrefix(path, "{") {
		return strings.Contains(path, "{") || strings.Contains(path, "}") || strings.TrimSpace(path) != path
	}
	closing := strings.IndexByte(path, '}')
	return closing >= 0 && closing != len(path)-1
}

func unsupportedTokenAt(input string, position int) bool {
	if position >= len(input) {
		return false
	}
	return strings.ContainsRune("$|?*:@", rune(input[position]))
}

func isIdentifierStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isIdentifierPart(value byte) bool {
	return isIdentifierStart(value) || isASCIIDigit(value)
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

func invalidExpression(sourceID, fieldName, message string, offset int) *ExtractionError {
	return &ExtractionError{SourceID: sourceID, FieldName: fieldName, Reason: ReasonInvalidExpression, Message: message, Offset: offset}
}

func unsupportedExpression(sourceID, fieldName, message string, offset int) *ExtractionError {
	return &ExtractionError{SourceID: sourceID, FieldName: fieldName, Reason: ReasonUnsupportedExpression, Message: message, Offset: offset}
}
