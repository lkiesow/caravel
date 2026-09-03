package assist

import (
	"encoding/json"
	"fmt"
)

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
	// Tags is a comma-separated list of free-text keywords, steered by the
	// vocabulary in the prompt. A string rather than an array so the whole
	// proposal pipeline stays string-shaped -- see Location.Tags.
	Tags string `json:"tags"`
	// Notes is markdown, in the user's locale.
	Notes string `json:"notes"`
	// Address is a postal address string for the geocoder to resolve. The
	// model supplies the words; internal/geocode supplies the position.
	Address string `json:"address"`
	// PlaceName is the searchable name of the place ("Kex Hostel, Reykjavik"),
	// used as the geocoder query when Address alone does not resolve.
	PlaceName string `json:"place_name"`
	// WikipediaTitle is a *lookup key*, exactly as Address is a lookup key for
	// the geocoder, and for the same reason: what reaches the user comes from
	// the upstream service, not from the model. The model naming an article is
	// checkable; the model naming an image URL would be a picture of the wrong
	// building with no visible tell.
	WikipediaTitle string      `json:"wikipedia_title"`
	Links          []modelLink `json:"links"`
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
  "required": ["title", "category", "tags", "notes", "address", "place_name", "wikipedia_title", "links"],
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
    "tags": {
      "type": "string",
      "description": "At most 5 short free-text keywords, comma-separated, saying what the place is or what somebody would filter a list by, such as: museum, unesco, free entry. Not adjectives and not the category again. Reuse the tags already in use on this trip, exactly as spelled, where they fit. Empty if none apply."
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
    "wikipedia_title": {
      "type": "string",
      "description": "The exact title of the Wikipedia article about this place, if it certainly has one. Used to look up a photograph. Empty if unsure or if the place is too small to have an article -- a wrong article gives a picture of somewhere else."
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

// maxSuggestions caps a trip-level answer.
//
// Six because the screen that reviews them has to be readable on a phone, and
// because the cost of a run rises with the number of places researched: every
// candidate is worth a search and a page read. It is a ceiling, not a target
// -- a run that finds three good places should offer three.
const maxSuggestions = 6

// modelSuggestions is the trip-level answer: several places, each in exactly
// the shape one place is proposed in.
//
// Reusing modelProposal as the element is the point. A separate flat struct
// would drift from it the first time a field was added to one and not the
// other, and everything downstream -- category validation, the geocoder, the
// link check, the cover -- already knows how to finish one of these.
type modelSuggestions struct {
	Suggestions []modelProposal `json:"suggestions"`
}

// suggestionsSchemaName names the schema on the wire, as proposalSchemaName
// does.
const suggestionsSchemaName = "trip_suggestions"

// suggestionsSchema wraps the proposal schema in an array rather than
// restating it, so the property block above has exactly one definition. The
// cap is written into the schema *and* enforced in Go: the json_object
// fallback (see provider.go) has no schema enforcement at all, which is the
// same reason category is validated rather than trusted.
var suggestionsSchema = json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["suggestions"],
  "properties": {
    "suggestions": {
      "type": "array",
      "minItems": 1,
      "maxItems": %d,
      "description": "The places you are proposing, each a distinct real place. Offer fewer rather than padding the list.",
      "items": %s
    }
  }
}`, maxSuggestions, proposalSchema))

// suggestionsFormat is the response_format for a trip-level final answer.
func suggestionsFormat() responseFormat {
	return responseFormat{Kind: formatJSONSchema, Name: suggestionsSchemaName, Schema: suggestionsSchema}
}

// proposalFormat is the response_format for the final answer.
func proposalFormat() responseFormat {
	return responseFormat{Kind: formatJSONSchema, Name: proposalSchemaName, Schema: proposalSchema}
}
