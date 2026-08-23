# Behind a reverse proxy

Caravel serves plain HTTP and does not terminate TLS. For anything reachable
beyond your own network, put a reverse proxy in front of it — Caddy, nginx,
Traefik — and let that handle certificates.

Everything on this page is what the code actually does, which in two places is
not what the convention would suggest.

## Set `X-Forwarded-Proto`

This is the one header Caravel reads, and it matters. The session cookie's
`Secure` attribute is set when the request arrived over TLS *or* when
`X-Forwarded-Proto: https` is present. Without it, a proxy terminating TLS
leaves Caravel believing the connection is plain HTTP, and the cookie goes out
without `Secure`.

Trusting that header is safe here because it can only ever *add* `Secure`, never
remove it — `r.TLS` is checked as well. The worst a spoofed header can do is make
a browser refuse to store a cookie set over plain HTTP.

=== "Caddy"

    ```caddy
    caravel.example.org {
        reverse_proxy localhost:8080
    }
    ```

    Caddy sets `X-Forwarded-Proto` itself. Nothing to configure.

=== "nginx"

    ```nginx
    server {
        server_name caravel.example.org;

        client_max_body_size 50m;

        location / {
            proxy_pass http://127.0.0.1:8080;
            proxy_set_header Host              $host;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        }
    }
    ```

=== "Traefik"

    Traefik sets the forwarded headers by default. The only thing to check is the
    request body limit if you have a `buffering` middleware in the chain.

## Rate limits stop being per-client

Caravel keys its rate limiters on the connection's remote address and **does not
read `X-Forwarded-For`**. Behind a proxy, every request appears to come from the
proxy, so the three per-address limits become instance-wide:

| Endpoint | Limit | Behind a proxy |
|---|---|---|
| Login and register | 10/minute per address | 10/minute for everyone together |
| Address search (`/api/geocode`) | 20/minute per address | 20/minute for everyone together |
| The assistant | 6/minute per address (configurable) | 6/minute for everyone together |

For a household instance this is mostly harmless and arguably a feature — the
login limiter still stops brute force, just globally. It is worth knowing about
if a dozen people use the map at once, because address search is the limit they
will actually meet. `CARAVEL_ASSIST_RATE_LIMIT` is settable; the other two are
not.

Setting `X-Forwarded-For` in the proxy does no harm and is good practice
regardless — Caravel simply does not read it today.

## Allow large enough request bodies

Uploads are capped by Caravel at **50 MB** for documents and **15 MB** for
images. A proxy with a smaller body limit will reject the upload first, and the
error the user sees will be the proxy's rather than a useful one. nginx defaults
to 1 MB, which is the common surprise.

## Headers Caravel sets, and two it does not

Every response carries:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: strict-origin-when-cross-origin`

There is **no `Strict-Transport-Security`** and **no `Content-Security-Policy`**.
HSTS belongs at the proxy, which is the thing that knows whether TLS is set up
and permanent; adding it is a good idea once your certificate renewal is
reliable. A CSP is simply not implemented.

## Sessions and CSRF, so you know what you are proxying

Sessions are opaque random tokens in an `HttpOnly`, `SameSite=Lax` cookie, and
only a SHA-256 hash of the token is stored server-side — a database leak does not
hand over usable sessions.

There is no separate CSRF token, deliberately. `SameSite=Lax` cookies are not
sent on cross-site `POST`/`PUT`/`PATCH`/`DELETE` requests, only on cross-site
top-level navigation, which is always a `GET` here — so a malicious site cannot
ride a logged-in session to change data. The request arrives with no session
cookie and gets a 401.

The consequence for a proxy is that Caravel must be served from **one origin**.
Serving the API and the frontend from different hostnames breaks the assumption
that makes `SameSite=Lax` sufficient.

## A sub-path is not supported

Caravel expects to own the root of its hostname. There is no configurable base
path, and the frontend's routes and asset URLs are absolute, so
`https://example.org/caravel/` does not work. Give it a hostname or a subdomain
of its own.
