// Package importer extracts run-sheet data from ACT! PDF exports using the
// Claude API, returning it in a structured form ready to preview and insert.
package importer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Data is the structured content of one run sheet PDF.
type Data struct {
	RunName   string     `json:"run_name"`
	Customers []Customer `json:"customers"`
}

type Customer struct {
	Name           string      `json:"name"`
	AddressLine    string      `json:"address_line"`
	Suburb         string      `json:"suburb"`
	Phone          string      `json:"phone"`
	MapRef         string      `json:"map_ref"`
	ServiceMinutes int64       `json:"service_minutes"`
	AccessNotes    string      `json:"access_notes"`
	GeneralNotes   string      `json:"general_notes"`
	Contacts       []Contact   `json:"contacts"`
	Zones          []Zone      `json:"zones"`
	Dispensers     []Dispenser `json:"dispensers"`
}

type Contact struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	IsPrimary bool   `json:"is_primary"`
	Phone     string `json:"phone"`
}

type Zone struct {
	Label       string `json:"label"`
	Area        string `json:"area"`
	AccessNotes string `json:"access_notes"`
}

type Dispenser struct {
	ZoneLabel           string `json:"zone_label"`
	SeqLabel            string `json:"seq_label"`
	Location            string `json:"location"`
	Model               string `json:"model"`
	Quantity            int64  `json:"quantity"`
	Fragrance           string `json:"fragrance"`
	FragranceNote       string `json:"fragrance_note"`
	RefillSizeMl        int64  `json:"refill_size_ml"`
	ServiceIntervalDays int64  `json:"service_interval_days"`
	Notes               string `json:"notes"`
}

// Enabled reports whether the import feature can run (API key configured).
func Enabled() bool { return os.Getenv("ANTHROPIC_API_KEY") != "" }

const extractPrompt = `This PDF is a pest-control/air-freshener service "run sheet" exported from ACT! CRM by Ecomist. It lists customer sites to visit, each with contacts, access notes, optional ZONE groupings, and numbered dispenser lines like:
"1. Foyer Area, around from Reception. Eco MAXI x 1. O/N/See Below650ml x 1. (70 Days) - 2580"

Extract ALL of it into the JSON schema. Rules:
- run_name: from the document title or window/header; if absent, invent a short sensible name from the suburb(s).
- One customers[] entry per company block (blocks are separated by sign-off sections with "Contact Name:.... Client Signature:....").
- suburb: the locality in CAPS from the address; address_line is the street part only.
- access_notes: door codes, "Need ladder", "call day before", service-day/time restrictions - anything a technician needs to get in.
- general_notes: fragrance rotation instructions and similar standing notes.
- contacts: the "Scheduled With" person has is_primary=true; other named people (e.g. "Maintenance Manager: Stuart") become extra contacts with their role.
- zones: each "ZONE n" heading with its area (e.g. "GROUND FLOOR", "APPLES WING") and any zone-specific door/lift codes. Sites without zones: empty array.
- dispensers: one entry per numbered line. zone_label must exactly match a zones[].label (or "" if the site has no zones). seq_label keeps the printed number/range ("1", "9 - 10"). quantity is the unit count on the line (ranges like "9 - 10 ... x 2" mean 2). model is the cleaned device name ("Eco Maxi", "Eco Midi", "Eco Midi Pro", "Eco Pro C", "Eco 6"). fragrance is the name after O/N/ with the trailing size removed ("Brandon", "Baby Talc", "NIK"); when the sheet says "See Below" or "Various", leave fragrance empty and put that text in fragrance_note. refill_size_ml from "650ml" (0 if absent). service_interval_days from "(70 Days)" (0 if absent). notes gets operating hours, "Cont 1"-style settings, and per-line door codes.
- Use 0 for unknown numbers and "" for unknown strings. service_minutes defaults to 15 when the sheet shows "15 minutes".
- Do not invent data; transcribe faithfully.`

var extractSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"run_name", "customers"},
	"properties": map[string]any{
		"run_name": map[string]any{"type": "string"},
		"customers": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required": []string{"name", "address_line", "suburb", "phone", "map_ref",
					"service_minutes", "access_notes", "general_notes", "contacts", "zones", "dispensers"},
				"properties": map[string]any{
					"name":            map[string]any{"type": "string"},
					"address_line":    map[string]any{"type": "string"},
					"suburb":          map[string]any{"type": "string"},
					"phone":           map[string]any{"type": "string"},
					"map_ref":         map[string]any{"type": "string"},
					"service_minutes": map[string]any{"type": "integer"},
					"access_notes":    map[string]any{"type": "string"},
					"general_notes":   map[string]any{"type": "string"},
					"contacts": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"name", "role", "is_primary", "phone"},
							"properties": map[string]any{
								"name":       map[string]any{"type": "string"},
								"role":       map[string]any{"type": "string"},
								"is_primary": map[string]any{"type": "boolean"},
								"phone":      map[string]any{"type": "string"},
							},
						},
					},
					"zones": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"label", "area", "access_notes"},
							"properties": map[string]any{
								"label":        map[string]any{"type": "string"},
								"area":         map[string]any{"type": "string"},
								"access_notes": map[string]any{"type": "string"},
							},
						},
					},
					"dispensers": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required": []string{"zone_label", "seq_label", "location", "model", "quantity",
								"fragrance", "fragrance_note", "refill_size_ml", "service_interval_days", "notes"},
							"properties": map[string]any{
								"zone_label":            map[string]any{"type": "string"},
								"seq_label":             map[string]any{"type": "string"},
								"location":              map[string]any{"type": "string"},
								"model":                 map[string]any{"type": "string"},
								"quantity":              map[string]any{"type": "integer"},
								"fragrance":             map[string]any{"type": "string"},
								"fragrance_note":        map[string]any{"type": "string"},
								"refill_size_ml":        map[string]any{"type": "integer"},
								"service_interval_days": map[string]any{"type": "integer"},
								"notes":                 map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	},
}

// Extract sends the PDF to Claude and returns the parsed run-sheet data.
func Extract(ctx context.Context, pdf []byte) (*Data, error) {
	if !Enabled() {
		return nil, fmt.Errorf("PDF import is not configured (ANTHROPIC_API_KEY missing)")
	}
	client := anthropic.NewClient(option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))

	b64 := base64.StdEncoding.EncodeToString(pdf)
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	params := anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeOpus4_8,
		MaxTokens: 64000,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		OutputConfig: anthropic.OutputConfigParam{
			Format: anthropic.JSONOutputFormatParam{Schema: extractSchema},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{Data: b64}),
				anthropic.NewTextBlock(extractPrompt),
			),
		},
	}

	// Stream to avoid HTTP timeouts on large sheets; accumulate the message.
	stream := client.Messages.NewStreaming(ctx, params)
	message := anthropic.Message{}
	for stream.Next() {
		if err := message.Accumulate(stream.Current()); err != nil {
			return nil, fmt.Errorf("accumulate: %w", err)
		}
	}
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("claude api: %w", err)
	}
	if message.StopReason == anthropic.StopReasonRefusal {
		return nil, fmt.Errorf("the model declined to process this document")
	}

	var text strings.Builder
	for _, block := range message.Content {
		if b, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	var data Data
	if err := json.Unmarshal([]byte(text.String()), &data); err != nil {
		return nil, fmt.Errorf("parse extraction: %w", err)
	}
	if len(data.Customers) == 0 {
		return nil, fmt.Errorf("no customers found in the PDF - is it a run sheet?")
	}
	return &data, nil
}
