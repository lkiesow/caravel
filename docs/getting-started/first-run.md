# First run

A fresh Caravel has no accounts, and a Caravel with no accounts always allows a
registration. So the first thing to do is register — that account becomes the
admin.

## Create the first account

1. Open <http://localhost:8080>.
2. Press **Create one** under the login form, and register a username, display
   name and password.
3. You are logged in, and you are the administrator.

Once that account exists, registration closes again: an anonymous visitor gets
no register link, and a registration attempt is refused.

!!! info "There is no signup environment variable"

    Earlier versions had a `CARAVEL_OPEN_SIGNUP`. It is gone — registration is
    an instance setting stored in the database and changed from the
    Administration screen. Two sources for one answer means the admin screen can
    show something the server does not believe, and no user can tell which one
    is lying.

## Letting other people in

Everything below is on the **Administration** screen, which is in the user menu
(top right) for administrators only.

**Create their account for them.** Under *Add an account*, enter a username, a
display name and a password, and tell them the password — they can change it in
their own settings. This is the most controlled route and needs nothing switched
on.

**Open registration for a while.** Under *Registration*, tick **Anyone can
register an account**, let the people you want in register themselves, then
untick it. Convenient for a group that has no accounts yet.

!!! warning

    Open registration is exactly what it says: anyone who can reach the URL can
    create an account. On an instance reachable from the internet, untick it
    again as soon as everyone is in.

Either way, having an account is not the same as being on a trip. To share a
trip, open it, go to its **People on this trip** section and add them by their
**exact username** — they need an account on this Caravel already. Each person
is an editor (can change the trip) or a viewer (read-only).

## What to set up next

- **Address search** already works, against OpenStreetMap's Nominatim. If you
  will use it heavily, read [Address
  search](../configuration/address-search.md) — the usage policy matters and
  self-hosting is an option.
- **The assistant** is off until you configure it, and needs a model endpoint
  you host or pay for. See [The assistant](../configuration/assistant.md).
- **Put it behind TLS** if it is reachable from outside your own network. See
  [Behind a reverse proxy](../running/reverse-proxy.md).
- **Set up backups** before there is anything to lose. See [Backup and
  restore](../running/backup.md).
