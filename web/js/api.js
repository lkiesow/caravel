class ApiError extends Error {
  constructor(status, body) {
    super(body?.error || `request failed with status ${status}`);
    this.status = status;
    this.body = body;
  }
}

async function request(method, path, body) {
  const res = await fetch(`/api${path}`, {
    method,
    headers: body ? { "Content-Type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    credentials: "same-origin",
  });

  if (res.status === 204) return null;

  const data = await res.json().catch(() => null);
  if (!res.ok) throw new ApiError(res.status, data);
  return data;
}

// POST that reads a Server-Sent Events response, calling onEvent per event.
//
// Not EventSource: that can only issue a GET with no body, and the one
// endpoint that streams here (the assistant) has to send the location the user
// is looking at. fetch() also gives cancellation for free through an
// AbortController, which the server sees as the request context ending.
//
// Resolves when the stream closes. An abort surfaces as the caller's own
// AbortError, which callers are expected to recognise rather than report.
async function postStream(path, body, { signal, onEvent } = {}) {
  const res = await fetch(`/api${path}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
    credentials: "same-origin",
    signal,
  });

  // A refusal still arrives as ordinary JSON with a status, since it happens
  // before the stream opens. Once it is open the status is 200 and failures
  // come through as events instead.
  if (!res.ok) {
    const data = await res.json().catch(() => null);
    throw new ApiError(res.status, data);
  }
  if (!res.body) throw new ApiError(res.status, { error: "streaming is not supported by this browser" });

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    // stream:true so a multi-byte character split across two chunks is not
    // mangled -- entirely possible here, since page titles reach these events.
    buffer += decoder.decode(value, { stream: true });

    // Events are terminated by a blank line. Normalised first because the
    // separator is \r\n\r\n if anything in the path rewrites line endings.
    buffer = buffer.replace(/\r\n/g, "\n");
    let split;
    while ((split = buffer.indexOf("\n\n")) !== -1) {
      const chunk = buffer.slice(0, split);
      buffer = buffer.slice(split + 2);
      const event = parseSSEChunk(chunk);
      if (event) onEvent?.(event);
    }
  }
}

function parseSSEChunk(chunk) {
  let name = "message";
  const data = [];
  for (const line of chunk.split("\n")) {
    if (line.startsWith("event:")) name = line.slice(6).trim();
    else if (line.startsWith("data:")) data.push(line.slice(5).trim());
    // Anything else (a comment line, an id) is not used here and ignored.
  }
  if (!data.length) return null;
  try {
    return { name, data: JSON.parse(data.join("\n")) };
  } catch {
    // A malformed event is dropped rather than killing the stream: the run is
    // still going and the next event may well be the proposal.
    return null;
  }
}

export const api = {
  get: (path) => request("GET", path),
  post: (path, body) => request("POST", path, body),
  put: (path, body) => request("PUT", path, body),
  patch: (path, body) => request("PATCH", path, body),
  delete: (path) => request("DELETE", path),
  postStream,
};

export { ApiError };
