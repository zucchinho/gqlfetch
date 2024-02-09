package gqlfetch

import (
	"encoding/json"
	"fmt"

	"github.com/vektah/gqlparser/ast"
)

type introspectionResults struct {
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Data struct {
		Schema introspectionSchema `json:"__schema"`
	} `json:"data"`
}

type introspectionSchema struct {
	QueryType    ast.Definition                     `json:"queryType"`
	MutationType ast.Definition                     `json:"mutationType"`
	Types        []introspectionTypeDefinition      `json:"types"`
	Directives   []introspectionDirectiveDefinition `json:"directives"`
}

type introspectionTypeDefinition struct {
	Kind          ast.DefinitionKind        `json:"kind"`
	Name          string                    `json:"name"`
	Description   string                    `json:"description"`
	Fields        []introspectedTypeField   `json:"fields"`
	InputFields   []introspectionInputField `json:"inputFields"`
	Interfaces    []ast.Definition          `json:"interfaces"`
	EnumValues    json.RawMessage           `json:"enumValues"`
	PossibleTypes json.RawMessage           `json:"possibleTypes"`
}

type introspectedTypeField struct {
	Name              string                    `json:"name"`
	Description       string                    `json:"description"`
	Args              []introspectionInputField `json:"args"`
	Type              *introspectedType         `json:"type"`
	IsDeprecated      bool                      `json:"isDeprecated"`
	DeprecationReason interface{}               `json:"deprecationReason"`
}

type introspectionDirectiveDefinition struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Locations   []ast.DirectiveLocation `json:"locations"`
	Args        []struct {
		Name         string            `json:"name"`
		Description  string            `json:"description"`
		Type         *introspectedType `json:"type"`
		DefaultValue interface{}       `json:"defaultValue"`
	} `json:"args"`
}

type introspectionInputField struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Type         *introspectedType `json:"type"`
	DefaultValue interface{}       `json:"defaultValue"`
}

type introspectedType struct {
	Kind   introspectionTypeKind `json:"kind"`
	Name   *string               `json:"name"`
	OfType *introspectedType     `json:"ofType"`
}

type introspectionTypeKind string

const (
	NON_NULL introspectionTypeKind = "NON_NULL"
	LIST     introspectionTypeKind = "LIST"
	OBJECT   introspectionTypeKind = "OBJECT"
)

func introspectionTypeToAstType(typ *introspectedType) (*ast.Type, error) {
	var res ast.Type
	if typ.OfType == nil {
		res.NamedType = *typ.Name
		return &res, nil
	}

	switch typ.Kind {
	case NON_NULL:
		astType, err := introspectionTypeToAstType(typ.OfType)
		if err != nil {
			return nil, fmt.Errorf("convert introspection type to AST type: %w\n%v", err, typ.OfType)
		}
		res = *astType
		res.NonNull = true
		return &res, nil
	case LIST:
		astType, err := introspectionTypeToAstType(typ.OfType)
		if err != nil {
			return nil, fmt.Errorf("convert introspection type to AST type: %w\n%v", err, typ.OfType)
		}
		res.Elem = astType
		return &res, nil
	default:
		return nil, fmt.Errorf("type kind unknown: %s", typ.Kind)
	}
}

var (
	excludeScalarTypes = []string{"ID", "Int", "String", "Float", "Boolean"}
	excludeDirectives  = []string{"deprecated", "include", "skip"}
)

func containsStr(needle string, hay []string) bool {
	for _, s := range hay {
		if needle == s {
			return true
		}
	}
	return false
}
