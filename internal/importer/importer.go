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

// Note: this deliberately does NOT use the structured-outputs output_config.
// With output_config.format set, Opus returns a schema-valid but EMPTY
// extraction for these sheets (customers: []); with the schema written into
// the prompt instead it transcribes everything faithfully.
const extractPrompt = `This PDF is a pest-control/air-freshener service "run sheet" exported from ACT! CRM by Ecomist. It lists customer sites to visit, each with contacts, access notes, optional ZONE groupings, and numbered dispenser lines like:
"1. Foyer Area, around from Reception. Eco MAXI x 1. O/N/See Below650ml x 1. (70 Days) - 2580"

Extract ALL of it and return ONLY a JSON object matching this schema (no prose, no code fences):

{"run_name": string, "customers": [{"name": string, "address_line": string, "suburb": string, "phone": string, "map_ref": string, "service_minutes": integer, "access_notes": string, "general_notes": string, "contacts": [{"name": string, "role": string, "is_primary": boolean, "phone": string}], "zones": [{"label": string, "area": string, "access_notes": string}], "dispensers": [{"zone_label": string, "seq_label": string, "location": string, "model": string, "quantity": integer, "fragrance": string, "fragrance_note": string, "refill_size_ml": integer, "service_interval_days": integer, "notes": string}]}]}

Rules:
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
	// Tolerate code fences or prose around the JSON object.
	raw := text.String()
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON found in the extraction response")
	}
	var data Data
	if err := json.Unmarshal([]byte(raw[start:end+1]), &data); err != nil {
		return nil, fmt.Errorf("parse extraction: %w", err)
	}
	if len(data.Customers) == 0 {
		return nil, fmt.Errorf("no customers found in the PDF - is it a run sheet?")
	}
	var lines int
	for _, c := range data.Customers {
		lines += len(c.Dispensers)
	}
	if lines == 0 {
		return nil, fmt.Errorf("no dispenser lines found in the PDF - is it a run sheet?")
	}
	return &data, nil
}
