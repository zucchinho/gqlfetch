package gqlfetch

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vektah/gqlparser/ast"
)

//go:embed introspect.graphql
var introspectSchema string

type BuildClientSchemaOptions struct {
	Endpoint        string
	Method          string
	Headers         http.Header
	WithoutBuiltins bool
}

func BuildClientSchema(ctx context.Context, endpoint string, withoutBuiltins bool) (string, error) {
	return BuildClientSchemaWithOptions(ctx, BuildClientSchemaOptions{
		Endpoint:        endpoint,
		Method:          http.MethodPost,
		Headers:         make(http.Header),
		WithoutBuiltins: withoutBuiltins,
	})
}

func BuildClientSchemaWithHeaders(ctx context.Context, endpoint string, headers http.Header, withoutBuiltins bool) (string, error) {
	return BuildClientSchemaWithOptions(ctx, BuildClientSchemaOptions{
		Endpoint:        endpoint,
		Method:          http.MethodPost,
		Headers:         headers,
		WithoutBuiltins: withoutBuiltins,
	})
}

func BuildClientSchemaWithOptions(ctx context.Context, options BuildClientSchemaOptions) (string, error) {
	buffer := new(bytes.Buffer)
	if err := json.NewEncoder(buffer).Encode(struct {
		Query string `json:"query"`
	}{Query: introspectSchema}); err != nil {
		return "", fmt.Errorf("failed to prepare introspection query request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, options.Method, options.Endpoint, buffer)
	if err != nil {
		return "", fmt.Errorf("failed to create query request: %w", err)
	}

	// If no headers are provided, create an empty header map, so we can add the content type header
	if options.Headers == nil {
		options.Headers = make(http.Header)
	}
	req.Header = http.Header(options.Headers)
	req.Header.Add("Content-Type", "application/json")

	client := http.Client{Timeout: 2 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var schemaResponse introspectionResults
	err = json.NewDecoder(res.Body).Decode(&schemaResponse)
	if err != nil {
		return "", err
	}

	if len(schemaResponse.Errors) != 0 {
		var errs []string
		for _, err := range schemaResponse.Errors {
			errs = append(errs, err.Message)
		}
		return "", errors.New("encountered the following GraphQL errors: " + strings.Join(errs, ","))
	}

	return printSchema(schemaResponse.Data.Schema, options.WithoutBuiltins), nil
}

func printSchema(schema introspectionSchema, withoutBuiltins bool) string {
	sb := &strings.Builder{}

	printDirectives(sb, schema.Directives, withoutBuiltins)
	printTypes(sb, schema.Types, withoutBuiltins)

	return sb.String()
}

func printDirectives(sb *strings.Builder, directives []introspectionDirectiveDefinition, withoutBuiltins bool) error {
	for _, directive := range directives {
		if withoutBuiltins && containsStr(directive.Name, excludeDirectives) {
			continue
		}
		printDescription(sb, directive.Description)
		sb.WriteString(fmt.Sprintf("directive @%s", directive.Name))
		if len(directive.Args) > 0 {
			sb.WriteString("(\n")
			for _, arg := range directive.Args {
				printDescription(sb, arg.Description)
				astType, err := introspectionTypeToAstType(arg.Type)
				if err != nil {
					return fmt.Errorf("convert introspection type to AST type: %w\n%v", err, arg.Type)
				}
				sb.WriteString(fmt.Sprintf("\t%s: %s\n", arg.Name, astType.String()))
			}
			sb.WriteString(")")
		}

		sb.WriteString(" on ")
		for i, location := range directive.Locations {
			sb.WriteString(string(location))
			if i < len(directive.Locations)-1 {
				sb.WriteString(" | ")
			}
		}
		sb.WriteString("\n")
		sb.WriteString("\n")
	}

	return nil
}

func printTypes(sb *strings.Builder, types []introspectionTypeDefinition, withoutBuiltins bool) error {
	for _, typ := range types {
		if strings.HasPrefix(typ.Name, "__") {
			continue
		}
		if withoutBuiltins && containsStr(typ.Name, excludeScalarTypes) && typ.Kind == ast.Scalar {
			continue
		}
		printDescription(sb, typ.Description)

		switch typ.Kind {

		case ast.Object:
			sb.WriteString(fmt.Sprintf("type %s ", typ.Name))
			if len(typ.Interfaces) > 0 {
				sb.WriteString("implements ")
				for i, intface := range typ.Interfaces {
					sb.WriteString(intface.Name)
					if i < len(typ.Interfaces)-1 {
						sb.WriteString(" & ")
					}
				}
			}
			sb.WriteString("{\n")
			for _, field := range typ.Fields {
				printDescription(sb, field.Description)
				sb.WriteString(fmt.Sprintf("\t%s", field.Name))
				if len(field.Args) > 0 {
					sb.WriteString("(\n")
					for _, arg := range field.Args {
						printDescription(sb, arg.Description)
						astType, err := introspectionTypeToAstType(arg.Type)
						if err != nil {
							return fmt.Errorf("convert introspection type to AST type: %w\n%v", err, arg.Type)
						}
						sb.WriteString(fmt.Sprintf("\t\t%s: %s\n", arg.Name, astType.String()))
					}
					sb.WriteString("\t)")
				}
				astType, err := introspectionTypeToAstType(field.Type)
				if err != nil {
					return fmt.Errorf("convert introspection type to AST type: %w\n%v", err, field.Type)
				}
				sb.WriteString(fmt.Sprintf(": %s\n", astType.String()))
			}
			sb.WriteString("}")

		case ast.Union:
			sb.WriteString(fmt.Sprintf("union %s =", typ.Name))
			var possible []*introspectedType
			if err := json.Unmarshal(typ.PossibleTypes, &possible); err != nil {
				return err
			}
			for i, typ := range possible {
				astType, err := introspectionTypeToAstType(typ)
				if err != nil {
					return fmt.Errorf("convert introspection type to AST type: %w\n%v", err, typ)
				}
				sb.WriteString(astType.String())
				if i < len(possible)-1 {
					sb.WriteString(" | ")
				}
			}

		case ast.Enum:
			sb.WriteString(fmt.Sprintf("enum %s {\n", typ.Name))
			var enumValues ast.EnumValueList
			if err := json.Unmarshal(typ.EnumValues, &enumValues); err != nil {
				return fmt.Errorf("cannot unmarshal enum values: %w\n%v", err, typ.EnumValues)
			}
			for _, value := range enumValues {
				printDescription(sb, value.Description)
				sb.WriteString(fmt.Sprintf("\t%s\n", value.Name))
			}
			sb.WriteString("}")

		case ast.Scalar:
			sb.WriteString(fmt.Sprintf("scalar %s", typ.Name))

		case ast.InputObject:
			sb.WriteString(fmt.Sprintf("input %s {\n", typ.Name))
			for _, field := range typ.InputFields {
				printDescription(sb, typ.Description)
				astType, err := introspectionTypeToAstType(field.Type)
				if err != nil {
					return fmt.Errorf("convert introspection type to AST type: %w\n%v", err, field.Type)
				}
				sb.WriteString(fmt.Sprintf("\t%s: %s\n", field.Name, astType.String()))
			}
			sb.WriteString("}")

		case ast.Interface:
			printInterface(sb, typ)
		default:
			return fmt.Errorf(fmt.Sprintf("not handling kind: %v", typ.Kind))
		}
		sb.WriteString("\n")
		sb.WriteString("\n")
	}

	return nil
}

func printDescription(sb *strings.Builder, description string) {
	if description != "" {
		sb.WriteString(fmt.Sprintf(`"""%s"""`, description))
		sb.WriteString("\n")
	}
}

func printInterface(sb *strings.Builder, typ introspectionTypeDefinition) error {
	if typ.Kind != ast.Interface {
		return fmt.Errorf("cannot print %v as %v", typ.Kind, ast.Interface)
	}

	sb.WriteString(fmt.Sprintf("interface %s {\n", typ.Name))
	for _, field := range typ.Fields {
		printDescription(sb, typ.Description)
		sb.WriteString(fmt.Sprintf("\t%s", field.Name))
		if len(field.Args) > 0 {
			sb.WriteString("(\n")
			for _, arg := range field.Args {
				astType, err := introspectionTypeToAstType(arg.Type)
				if err != nil {
					return fmt.Errorf("convert introspection type to AST type: %w\n%v", err, arg.Type)
				}
				sb.WriteString(fmt.Sprintf("\t\t%s: %s\n", arg.Name, astType.String()))
			}
			sb.WriteString("\t)")
		}
		astType, err := introspectionTypeToAstType(field.Type)
		if err != nil {
			return fmt.Errorf("convert introspection type to AST type: %w\n%v", err, field.Type)
		}
		sb.WriteString(fmt.Sprintf(": %s\n", astType.String()))
	}
	sb.WriteString("}")

	return nil
}
