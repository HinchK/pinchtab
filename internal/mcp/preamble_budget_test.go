package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

const preambleBudgetPath = "testdata/preamble_budget.json"

const toolPayloadSurface = "mcp:allTools"

var fileSurfaces = []string{"skills/pinchtab-mcp/SKILL.md", "skills/pinchtab/SKILL.md"}

type preambleBudget struct {
	Surface  string `json:"surface"`
	MaxBytes int    `json:"maxBytes"`
	TakenOn  string `json:"takenOn"`
	Commit   string `json:"commit"`
}

type toolComposition struct {
	tools            int
	descriptionBytes int
	repeatedBytes    int
}

type preambleMeasurement struct {
	surface     string
	bytes       int
	composition *toolComposition
}

func (m preambleMeasurement) approxTokens() int { return m.bytes / 4 }

func percentOf(part, whole int) string {
	if whole == 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", part*100/whole)
}

func descriptionStrings(tools []mcp.Tool) []string {
	var texts []string
	for _, tool := range tools {
		if tool.Description != "" {
			texts = append(texts, tool.Description)
		}
		for _, property := range tool.InputSchema.Properties {
			fields, ok := property.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := fields["description"].(string); ok && text != "" {
				texts = append(texts, text)
			}
		}
	}
	return texts
}

func repeatedBytes(texts []string) int {
	seen := make(map[string]bool, len(texts))
	repeated := 0
	for _, text := range texts {
		if seen[text] {
			repeated += len(text)
			continue
		}
		seen[text] = true
	}
	return repeated
}

func toolDescriptionBytes(tools []mcp.Tool) int {
	total := 0
	for _, tool := range tools {
		total += len(tool.Description)
	}
	return total
}

func measureToolPayload(t *testing.T) preambleMeasurement {
	t.Helper()
	tools := allTools()
	payload, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("cannot serialize %s, so the preamble an MCP agent loads is unmeasurable: %v", toolPayloadSurface, err)
	}
	return preambleMeasurement{
		surface: toolPayloadSurface,
		bytes:   len(payload),
		composition: &toolComposition{
			tools:            len(tools),
			descriptionBytes: toolDescriptionBytes(tools),
			repeatedBytes:    repeatedBytes(descriptionStrings(tools)),
		},
	}
}

func measureFile(t *testing.T, surface string) preambleMeasurement {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", surface))
	if err != nil {
		t.Fatalf("cannot read %s, so the preamble it costs is unmeasurable: %v", surface, err)
	}
	return preambleMeasurement{surface: surface, bytes: len(body)}
}

func loadPreambleBudgets(t *testing.T) map[string]preambleBudget {
	t.Helper()
	body, err := os.ReadFile(preambleBudgetPath)
	if err != nil {
		t.Fatalf("cannot read %s, so nothing holds the preamble to a recorded number: %v", preambleBudgetPath, err)
	}
	var recorded []preambleBudget
	if err := json.Unmarshal(body, &recorded); err != nil {
		t.Fatalf("cannot parse %s: %v", preambleBudgetPath, err)
	}
	budgets := make(map[string]preambleBudget, len(recorded))
	for _, budget := range recorded {
		budgets[budget.Surface] = budget
	}
	return budgets
}

func renderPreambleReport(measurements []preambleMeasurement, budgets map[string]preambleBudget) string {
	const row = "%-30s %6s %8s %8s %8s %8s %8s\n"
	var report strings.Builder
	report.WriteString("agent preamble\n")
	fmt.Fprintf(&report, row, "surface", "tools", "bytes", "~tokens", "desc", "repeat", "budget")
	for _, measurement := range measurements {
		tools, description, repeat := "-", "-", "-"
		if composition := measurement.composition; composition != nil {
			tools = strconv.Itoa(composition.tools)
			description = percentOf(composition.descriptionBytes, measurement.bytes)
			repeat = percentOf(composition.repeatedBytes, measurement.bytes)
		}
		fmt.Fprintf(&report, row,
			measurement.surface,
			tools,
			strconv.Itoa(measurement.bytes),
			strconv.Itoa(measurement.approxTokens()),
			description,
			repeat,
			strconv.Itoa(budgets[measurement.surface].MaxBytes),
		)
	}
	return report.String()
}

func TestAgentPreambleStaysWithinBudget(t *testing.T) {
	measurements := []preambleMeasurement{measureToolPayload(t)}
	for _, surface := range fileSurfaces {
		measurements = append(measurements, measureFile(t, surface))
	}

	budgets := loadPreambleBudgets(t)
	t.Log("\n" + renderPreambleReport(measurements, budgets))

	for _, measurement := range measurements {
		budget, recorded := budgets[measurement.surface]
		if !recorded {
			t.Errorf("%s is measured but carries no budget in %s; record today's number there so the surface can only ratchet down", measurement.surface, preambleBudgetPath)
			continue
		}
		if measurement.bytes <= budget.MaxBytes {
			continue
		}
		over := measurement.bytes - budget.MaxBytes
		t.Errorf("%s is %d bytes (~%d tokens), %d bytes (~%d tokens) over the budget of %d recorded on %s at %s; shrink the surface, or raise the budget as a deliberate line in this diff",
			measurement.surface, measurement.bytes, measurement.approxTokens(), over, over/4, budget.MaxBytes, budget.TakenOn, budget.Commit)
	}
}
