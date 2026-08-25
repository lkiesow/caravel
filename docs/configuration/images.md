# Finding an image

Every trip and every location can carry a photo. Beside uploading one or pasting
a URL, the editor can **search for one** — and, when the assistant is on, offer
one by itself.

Neither needs configuration. The search always includes Wikipedia, which wants
no key and no account, so a stock instance has a working image picker.

| Variable | Default | Purpose |
|---|---|---|
| `CARAVEL_WIKIMEDIA_URL` | *(empty)* | Pins the Wikipedia API endpoint to a mirror. Empty — the normal case — sends each lookup to the Wikipedia edition matching the user's own language |

## Two sources, and why they are labelled separately

**Wikipedia** searches the edition for the language the user is reading Caravel
in. That matters more than it looks: the German article is *Brandenburger Tor*
and the English one is *Brandenburg Gate*, and Osnabrück's Heger Tor has a German
article and no English one — an instance pinned to English would find nothing for
a whole class of places its users care about.

Results arrive with the photographer and the licence, and both are stored with
the image and shown wherever it appears. A freely licensed photograph is not an
unencumbered one, and the attribution cannot be recovered later if it is not kept
at the moment the picture is saved.

**A web image search** runs as well when `CARAVEL_SEARCH_PROVIDER` names a
backend that can do one — `serper` or `ddgs`; Ollama Cloud has no images endpoint
and simply contributes nothing. Its coverage is far better for hotels and
restaurants, which Wikipedia has never heard of.

What it cannot tell you is the licence: a search engine reports where a picture
was found and nothing about the terms it may be used on. The two sets are
therefore shown as separate, labelled groups rather than one merged list, and the
web group carries a warning saying so. Checking before publishing is yours.

!!! note "Search without the assistant"

    `CARAVEL_SEARCH_PROVIDER` used to be refused unless `CARAVEL_LLM_URL` was set
    too, because nothing else in the app searched the web. Since the image picker
    does, the two are independent: a search provider on its own is a working
    configuration, and gives you image search with no LLM anywhere near it.

## Thumbnails are loaded by the browser

The grid of candidates hotlinks each thumbnail from wherever it lives —
Wikimedia, a search engine's cache, or the site the picture is on. So **the
people using your instance disclose their IP address to those hosts** while a
result grid is on screen, in the same way that loading map tiles does.

The alternative would be streaming every thumbnail through the instance, which
turns a picker into a proxy for arbitrary remote images. That trade did not seem
worth making for a grid that is on screen for a few seconds; if it matters to
you, leave the feature to Wikipedia alone by not configuring a search provider.

The **full-size** image is a different matter: when a candidate is picked, the
server fetches and stores it, exactly as it does for a pasted URL. Nothing on a
saved page is hotlinked.

## What the assistant does with this

The assistant proposes a cover as part of a run, from the page it read: the
`og:image` a venue's own site declares, falling back to the Wikipedia article's
lead image. See [the assistant](assistant.md).

Picking from a grid is deliberately **not** something it does. The model has no
vision, so it would be choosing a photograph by the words printed around it — and
a wrong-but-plausible picture of a place you have never been is a mistake with no
tell. Choosing between pictures is left to the person, which is what the search
control is for.
