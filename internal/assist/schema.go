package assist

import "encoding/json"

// The contract with the model: what it is asked to return, and the JSON Schema
// that describes it.
//
// modelProposal is deliberately *not* Proposal. Three fields the caller sees
// are absent here because the model is not allowed to supply them:
//
//   - coordinates, resolved from the address by internal/geocode, because a
//     hallucinated lat/lng is the one error with no visible tell;
//   - the current values in each Field, which come from the location itself;
//   - Sources, which are recorded from the tool calls the agent actually made
//     rather than from what the model claims it read.
type modelProposal struct {
	// Title is proposed only in ModePrompt, where there is nothing to enrich.
	Title string `json:"title"`
	// Category must be one of the three; validated in agent.go rather than
	// trusted, even though the schema also constrains it -- the json_object
	// fallback has no schema enforcement at all.
	Category string `json:"category"`
	// Type is free text steered by the vocabulary in the prompt.
	Type string `json:"type"`
	// Notes is markdown, in the user's locale.
	Notes string `json:"notes"`
	// Address is a postal address string for the geocoder to resolve. The
	// model supplies the words; internal/geocode supplies the position.
	Address string `json:"address"`
	// PlaceName is the searchable name of the place ("Kex Hostel, Reykjavik"),
	// used as the geocoder query when Address alone does not resolve.
	PlaceName string      `json:"place_name"`
	Links     []modelLink `json:"links"`
}

type modelLink struct {
	URL   string `json:"url"`
	Label string `json:"label"`
}

// proposalSchemaName names the schema on the wire. Servers surface it in
// errors, so it is worth being recognisable.
const proposalSchemaName = "location_proposal"

// proposalSchema is hand-written rather than generated, and lives next to the
// struct above so the two are edited together.
//
// Two constraints that are not stylistic: every property appears in "required"
// and additionalProperties is false. OpenAI's strict mode rejects a schema
// without both, and servers that merely imitate it behave better with them. A
// field the model has nothing to say about comes back as an empty string,
// which is why nothing here is nullable -- an empty string and a missing key
// mean the same thing to the caller, and one of them needs no extra branch.
var proposalSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "category", "type", "notes", "address", "place_name", "links"],
  "properties": {
    "title": {
      "type": "string",
      "description": "The name of the place. Empty when enriching a location that already has one."
    },
    "category": {
      "type": "string",
      "enum": ["site", "stay", "transport"],
      "description": "site for somewhere to visit, stay for accommodation, transport for a journey or terminal."
    },
    "type": {
      "type": "string",
      "description": "A short free-text tag such as museum, hotel, ferry. Reuse one of the values already in use on this trip where it fits."
    },
    "notes": {
      "type": "string",
      "description": "A few sentences of practical detail in markdown: what it is, why go, opening hours or booking notes if known. In the language requested. Empty if nothing useful was found."
    },
    "address": {
      "type": "string",
      "description": "Postal address as a single line. Never coordinates."
    },
    "place_name": {
      "type": "string",
      "description": "The searchable name and city, used to look up the position. Never coordinates."
    },
    "links": {
      "type": "array",
      "description": "Official or genuinely useful URLs. Only URLs actually seen in a tool result, never guessed.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["url", "label"],
        "properties": {
          "url": {"type": "string"},
          "label": {"type": "string"}
        }
      }
    }
  }
}`)

// proposalFormat is the response_format for the final answer.
func proposalFormat() responseFormat {
	return responseFormat{Kind: formatJSONSchema, Name: proposalSchemaName, Schema: proposalSchema}
}
