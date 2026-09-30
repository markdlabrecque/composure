package config

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

const testField = `{"id":"body","kind":"long_text","label":"Body","help_text":"","required":false,"order":10}`

func testDocument(field string) string {
	return `{"format_version":1,"content_types":[{"id":"page","label":"Page","fields":[` + field + `]}]}`
}

func TestDecodeStrictDefinition(t *testing.T) {
	valid := testDocument(testField)
	cases := map[string]string{
		"malformed":         `{"format_version":1,`,
		"trailing_document": valid + `{}`,
		"root_null":         `null`, "root_array": `[]`,
		"no_types":                        `{"format_version":1,"content_types":[]}`,
		"extra_type":                      strings.Replace(valid, `"content_types":[`, `"content_types":[{"id":"page","label":"Page","fields":[]},`, 1),
		"wrong_type_id":                   strings.Replace(valid, `"id":"page"`, `"id":"article"`, 1),
		"root_duplicate_decoded_property": strings.Replace(valid, `"format_version":1`, `"format_version":1,"\u0066ormat_version":1`, 1),
		"type_duplicate_decoded_property": strings.Replace(valid, `"id":"page"`, `"id":"page","\u0069d":"page"`, 1),
		"duplicate_decoded_property":      testDocument(strings.Replace(testField, `"id":"body"`, `"id":"body","\u0069d":"body"`, 1)),
		"duplicate_id":                    testDocument(testField + "," + strings.Replace(testField, `"order":10`, `"order":20`, 1)),
		"duplicate_order":                 testDocument(testField + "," + strings.Replace(testField, `"id":"body"`, `"id":"other"`, 1)),
	}
	for _, version := range []string{"0", "2", "999999999999999999999999999999999999999", "1.0", "1e0", "true", `"1"`} {
		cases["version_"+version] = strings.Replace(valid, `"format_version":1`, `"format_version":`+version, 1)
	}
	for _, id := range []string{"title", "path", "Body", "1body", "a-b", "é", strings.Repeat("a", 65), ""} {
		quoted, _ := json.Marshal(id)
		cases["id_"+id] = testDocument(strings.Replace(testField, `"body"`, string(quoted), 1))
	}
	for _, kind := range []string{"rich_text", "LONG_TEXT", ""} {
		cases["kind_"+kind] = testDocument(strings.Replace(testField, `"long_text"`, `"`+kind+`"`, 1))
	}
	for _, order := range []string{"0", "-1", "10.0", "1e1", "true", `"10"`} {
		cases["order_"+order] = testDocument(strings.Replace(testField, `"order":10`, `"order":`+order, 1))
	}
	for _, escape := range []string{`\uD800`, `\uDC00`, `\uDC00\uD800`, `\uD800x`, `\uD800\uD800`} {
		cases["surrogate_"+escape] = testDocument(strings.Replace(testField, `"Body"`, `"`+escape+`"`, 1))
	}
	cases["invalid_utf8"] = testDocument(strings.Replace(testField, "Body", string([]byte{0xff}), 1))
	// Every defined property is required, non-null and correctly typed. Test
	// each level independently so an earlier failure cannot hide a later rule.
	for _, level := range []string{"root", "type", "field"} {
		var obj map[string]any
		if err := json.Unmarshal([]byte(valid), &obj); err != nil {
			t.Fatal(err)
		}
		target := obj
		if level != "root" {
			target = obj["content_types"].([]any)[0].(map[string]any)
		}
		if level == "field" {
			target = target["fields"].([]any)[0].(map[string]any)
		}
		keys := make([]string, 0, len(target))
		for key := range target {
			keys = append(keys, key)
		}
		for _, key := range keys {
			original := target[key]
			for _, variant := range []string{"missing", "null", "wrong_type"} {
				switch variant {
				case "missing":
					delete(target, key)
				case "null":
					target[key] = nil
				case "wrong_type":
					if _, ok := original.(string); ok {
						target[key] = false
					} else {
						target[key] = "wrong"
					}
				}
				data, err := json.Marshal(obj)
				if err != nil {
					t.Fatal(err)
				}
				cases[level+"_"+key+"_"+variant] = string(data)
				target[key] = original
			}
		}
		target["extra"] = true
		data, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}
		cases[level+"_unknown_property"] = string(data)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(data)); err == nil {
				t.Errorf("Decode accepted invalid definition: %q", data)
			}
		})
	}
}

func TestDecodeValidDefinitionAndExactOrders(t *testing.T) {
	for _, order := range []string{"1", "9007199254740993", "9223372036854775808", "999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999", strings.Repeat("9", 400)} {
		name := order
		if len(name) > 350 {
			name = strconv.Itoa(len(name)) + "_digit_order"
		}
		t.Run(name, func(t *testing.T) {
			data := testDocument(strings.Replace(testField, `"order":10`, `"order":`+order, 1))
			doc, err := Decode([]byte(data))
			if err != nil {
				t.Fatalf("valid positive integer order rejected: %v", err)
			}
			encoded, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var raw struct {
				ContentTypes []struct {
					Fields []struct{ Order json.RawMessage } `json:"fields"`
				} `json:"content_types"`
			}
			if err := json.Unmarshal(encoded, &raw); err != nil {
				t.Fatal(err)
			}
			if got := string(raw.ContentTypes[0].Fields[0].Order); got != order {
				t.Errorf("order lost precision: %s, want %s", got, order)
			}
		})
	}
	for name, field := range map[string]string{
		"empty_fields":                  "",
		"empty_labels":                  strings.ReplaceAll(testField, `"Body"`, `""`),
		"max_id":                        strings.Replace(testField, `"body"`, `"`+strings.Repeat("a", 64)+`"`, 1),
		"unicode_scalar":                strings.Replace(testField, `"Body"`, `"\uD83D\uDE00"`, 1),
		"literal_replacement_character": strings.Replace(testField, `"Body"`, `"�"`, 1),
		"distinct_large_orders":         strings.Replace(testField, `"order":10`, `"order":9007199254740992`, 1) + "," + strings.Replace(strings.Replace(testField, `"body"`, `"other"`, 1), `"order":10`, `"order":9007199254740993`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode([]byte(testDocument(field))); err != nil {
				t.Errorf("valid definition rejected: %v", err)
			}
		})
	}
}
