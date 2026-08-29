# Behind a reverse proxy

Caravel serves plain HTTP and does not terminate TLS. For anything reachable
beyond your own network, put a reverse proxy in front of it — Caddy, nginx,
Traefik — and let that handle certificates.

Everything on this page is what the code actually does, including where that
is not quite what the convention would suggest.

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

## Set `X-Forwarded-For`

Caravel keys its rate limiters, and the address recorded against a session, on
who the request came from. Behind a proxy that means reading
`X-Forwarded-For` — without it every request looks like the proxy, and these
per-address limits become instance-wide:

| Endpoint | Limit |
|---|---|
| Login and register | 10/minute per address |
| Address search (`/api/geocode`) | 20/minute per address |
| Image search | 10/minute per address |
| The assistant | 6/minute per address (`CARAVEL_ASSIST_RATE_LIMIT`) |

The header is read **only when the machine Caravel is talking to is a trusted
proxy**, which by default means the private address space:

```
127.0.0.0/8  ::1/128  10.0.0.0/8  172.16.0.0/12  192.168.0.0/16
169.254.0.0/16  fe80::/10  fc00::/7
```

So the ordinary arrangements — a proxy on the same host, or elsewhere on your
LAN or a container network — work with no configuration beyond setting the
header in the proxy. An instance exposed directly to the internet is unaffected
too: the peer address is public, so it is not trusted and no header is read.

Set `CARAVEL_TRUSTED_PROXIES` when neither of those describes you:

| You want | Set it to |
|---|---|
| A proxy at a public address | that address or range, e.g. `203.0.113.7` |
| Several, or a mix | a comma-separated list: `10.9.0.0/16, 203.0.113.7` |
| To trust nothing at all | `none` |

Whatever you set **replaces** the defaults rather than adding to them, so
naming your own proxy also stops loopback and the private ranges being trusted.

!!! warning "Who can forge this"

    Trusting a network means trusting everyone on it. Someone who can already
    reach Caravel from a private address can send an `X-Forwarded-For` of their
    choosing and pick which bucket the rate limiters count them in. On a
    household instance that is nobody who is not already inside. If it is not
    your situation — a shared container host, a large office LAN — narrow
    `CARAVEL_TRUSTED_PROXIES` to your proxy alone.

`100.64.0.0/10` is **not** in the defaults, although some frameworks include it.
That is Tailscale's range, and on a tailnet those addresses are usually the
people using the app rather than a proxy in front of it.

`X-Real-IP` is not read at all. It carries a single address and no chain, so
there is no way to tell how many hops it crossed or who last wrote it.

## Allow large enough request bodies

Uploads are capped by Caravel at **50 MB**, for both documents and images. A
proxy with a smaller body limit will reject the upload first, and the error the
user sees will be the proxy's rather than a useful one. nginx defaults to 1 MB,
which is the common surprise.

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
