# The assistant

Filling in a location by hand means finding the address, the opening times, the
official page and the coordinates yourself. The assistant does that lookup and
proposes the answers.

It is **off until you configure it** — it needs a model endpoint you host or pay
for. See [The assistant](../configuration/assistant.md) for the setup.

![The assistant proposing fields](../assets/screenshots/assistant.png)

## Every suggestion is accepted per field

This is the whole design. The assistant does not fill the form in; it puts a
proposal next to each field, and each one is accepted or rejected on its own.
Anything that would replace text you already wrote is marked as such.

Nothing is written until you accept it, and nothing is saved until you press
**Save** — so the worst outcome of a bad run is pressing Cancel.

## What it can propose

The title, the category and tags, notes, an address, links, and coordinates.

Coordinates are the interesting case: **they never come from the model.** It
proposes an address, and the geocoder resolves that address into a position. A
plausible latitude and longitude 40km from the real hotel looks entirely correct
in the form and is wrong only on the map — the one error with no visible tell.

The pages the assistant used are listed with the suggestions so you can check
them. They are not saved.

## What it costs, and what bounds it

A run reads web pages, so it costs tokens at whatever your provider charges.
Every guard rail is an environment variable — token budget, turns, tool calls,
timeouts, runs per minute, runs at once — because these are the numbers an
operator wants to change the same day a bill surprises them. The defaults are
listed under [configuration](../configuration/assistant.md#limits).

Hitting a limit does not throw the run away: research stops and the assistant
answers with what it found.
