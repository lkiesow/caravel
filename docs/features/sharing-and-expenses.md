# Sharing and expenses

A trip with more than one person on it needs two things: everyone able to see the
plan, and a way to work out who owes whom at the end.

## Sharing a trip

![The members tab](../assets/screenshots/members.png)

Add someone by their **exact username** — they need an account on this Caravel
already, which is the point of a self-hosted app rather than a limitation.
There are two roles:

| Role | Can |
|---|---|
| **Editor** | Change anything on the trip, including ticking shared checklists |
| **Viewer** | Read everything shared with the trip, and change nothing |

The trip's owner cannot be removed, and removing somebody takes their access
immediately. If they had personal files or lists on the trip, Caravel says how
many will be deleted with them before you confirm — those are theirs, and nobody
else can see them.

## Expenses

![The expenses tab](../assets/screenshots/expenses.png)

Each trip has one currency, chosen when you create it. Every amount is stored as
a whole number of the currency's smallest unit — cents for EUR, whole yen for
JPY — so nothing is ever a fraction of a cent out.

An expense records what it was for, what it cost, **who paid** and **who it was
for**. By default it splits evenly between everyone on the trip; untick "Split
with everyone" and it pins to a subset, and the row then says so ("Only for …"),
because otherwise a total would not follow from the amounts in front of you.

Every expense on a trip is visible to everyone on it, viewers included. There is
deliberately no private expense: a hidden row in a shared ledger makes an
incorrect total look correct.

## Settling up

![The balances summary](../assets/screenshots/balances.png)

From who paid and who each expense was for, Caravel works out where everyone
stands and suggests a short set of payments that settles it — not every pairwise
debt, just the transfers that clear it.

Two deliberate choices are visible in that screenshot. An expense **nobody is
recorded as having paid** is reported separately rather than split between people
who may not owe it. And nothing here marks a debt as paid: the settle-up list is
advice, not a record.

Changing a trip's currency does not convert amounts already recorded, so a
foreign-currency purchase is best entered as the converted amount.
